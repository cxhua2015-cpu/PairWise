package metacatalog361

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, 1, 1, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameCharset(t *testing.T) {
	s := store(t)
	for _, n := range []string{"", "A", "a b", "a.b", "a/b", "é", "a+b"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-0_x", "0123456789ab"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestNameAndValueLimits(t *testing.T) {
	s := store(t) // MaxNameBytes 12, MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "0123456789abc", []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", bytes.Repeat([]byte("x"), 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "0123456789ab", bytes.Repeat([]byte("x"), 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	for _, op := range []Op{{Kind(0), "a", nil}, {Kind(3), "a", nil}, {Delete, "a", []byte("x")}} {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: got %v", op, e)
		}
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed")
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of a missing key would yield ErrNotFound, but the invalid
	// name later in the batch must be reported first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("v")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	r, e = s.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
	if snap := s.Snapshot(); snap.Generation != 1 || snap.NextRevision != 2 {
		t.Fatal(snap)
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r.Changed)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}}})
	b := s.Snapshot()
	// Exceeds record count only at the end.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")}, {Put, "c", []byte("1")}, {Put, "d", []byte("1")},
	}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e, s.Snapshot())
	}
	// Exceeds total value bytes only at the end.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e, s.Snapshot())
	}
	// Delete-then-put within one batch may exceed transiently but pass at end.
	_, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("12345678")}}})
	if e != nil {
		t.Fatal(e)
	}
}

func TestRevisionContinuityAndDeleteNoAlloc(t *testing.T) {
	s := store(t)
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3")}}})
	if r.Revision != 3 {
		t.Fatal(r)
	}
	rec, ok, _ := s.Get("c")
	if !ok || rec.Revision != 3 {
		t.Fatal(rec, ok)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be deleted")
	}
}

func TestGetErrors(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "a", []byte("1")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'z'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "1" {
		t.Fatal(rec)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%03d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte(j)}}}})
				if j%3 == 0 {
					_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
				}
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	var total int
	for _, r := range snap.Records {
		total += len(r.Value)
	}
	if total > 512 || len(snap.Records) > 128 {
		t.Fatal("capacity violated")
	}
	if snap.Generation == 0 || snap.NextRevision <= 1 {
		t.Fatal(snap)
	}
}

func TestConcurrentRevisionMonotonic(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	var w sync.WaitGroup
	revs := make([]uint64, 16)
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
			if e == nil {
				revs[i] = r.Revision
			}
		}()
	}
	w.Wait()
	seen := map[uint64]bool{}
	for _, r := range revs {
		if r == 0 || seen[r] {
			t.Fatal("duplicate or zero revision", revs)
		}
		seen[r] = true
	}
}
