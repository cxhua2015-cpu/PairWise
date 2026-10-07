package metacatalog391

import (
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
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestNameCharset(t *testing.T) {
	s := store(t)
	ok := []string{"a", "z9-_", "abc-def_123"}
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "é", "a/b", "UPPER"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestNameAndValueLengthLimits(t *testing.T) {
	s := store(t) // MaxNameBytes 12, MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abcdefghijkl", []byte("12345678")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abcdefghijklm", []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("123456789")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	for _, k := range []Kind{0, 3, 255} {
		if _, e := s.Apply(Batch{Ops: []Op{{Kind: k, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: got %v", k, e)
		}
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("generation changed on empty batch")
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if x.Generation != 1 {
		t.Fatal(x.Generation)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if x.Generation != 2 || s.Snapshot().Generation != 2 {
		t.Fatal(x.Generation)
	}
}

func TestDeleteNoRevisionAllocated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	x, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if x.Revision != 1 || s.Snapshot().NextRevision != 2 {
		t.Fatal(x, s.Snapshot())
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	// Intermediate states exceed limits; only the final state matters.
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12")},
		{Put, "b", []byte("34")},
		{Put, "c", []byte("56")}, // 3 records mid-batch
		{Delete, "a", nil},
		{Delete, "c", nil}, // back to 1 record, 2 bytes
	}})
	if e != nil {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 1 {
		t.Fatal(n)
	}
}

func TestCapacityExceededRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("345")}}}) // total 5 > 4
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "c", []byte("2")}}}) // 3 records > 2
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestOverwriteDoesNotCountTwice(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "a", []byte("1234")}}}); e != nil {
		t.Fatal(e)
	}
	r, _, _ := s.Get("a")
	if string(r.Value) != "1234" || r.Revision != 2 {
		t.Fatal(r)
	}
}

func TestDeleteThenPutSameName(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "a", []byte("2")}}})
	if e != nil || len(x.Changed) != 1 || x.Changed[0].Revision != 2 {
		t.Fatal(e, x)
	}
}

func TestGetMissingAndInvalid(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 16, MaxTotalValueBytes: 4096})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%03d", i)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 || snap.Generation != 32*51 || snap.NextRevision != 32*50+1 {
		t.Fatal(snap.Generation, snap.NextRevision, len(snap.Records))
	}
}

func TestConcurrentMonotonicRevisions(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	const n = 64
	revs := make(chan uint64, n)
	var w sync.WaitGroup
	for i := 0; i < n; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
			if e != nil {
				t.Error(e)
				return
			}
			revs <- x.Revision
		}()
	}
	w.Wait()
	close(revs)
	seen := map[uint64]bool{}
	for r := range revs {
		if seen[r] {
			t.Fatal("duplicate revision", r)
		}
		seen[r] = true
	}
	if len(seen) != n || !seen[1] || !seen[n] {
		t.Fatal(len(seen))
	}
}

func TestDoubleDeleteSameBatch(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Delete, "a", nil}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}
