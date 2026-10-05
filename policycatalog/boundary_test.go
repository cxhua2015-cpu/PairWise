package policycatalog

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	cases := []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, -1, 1, 1}, {1, 1, -1, 1}, {1, 1, 1, -1},
	}
	for _, o := range cases {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestNameCharset(t *testing.T) {
	s, _ := New(Options{MaxRecords: 16, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	good := []string{"a", "abc", "a1", "a-b", "a_b", "0", "z9-_"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q should be valid: %v", n, e)
		}
	}
	bad := []string{"", "A", "aB", "a b", "a.b", "a/b", "é", "a!", string(rune(0x7f))}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
	}
}

func TestNameAndValueLimits(t *testing.T) {
	s := store(t) // MaxNameBytes 12, MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abcdefghijkl", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abcdefghijklm", []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "ok", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "ok", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	for _, k := range []Kind{0, 3, 255} {
		if _, e := s.Apply(Batch{Ops: []Op{{k, "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: %v", k, e)
		}
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of missing name would fail with ErrNotFound, but the invalid
	// name later in the batch must surface ErrInvalidInput instead.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestDeleteKeepsRevision(t *testing.T) {
	s := store(t)
	r1, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	if e != nil || r1.Revision != 1 {
		t.Fatal(e, r1)
	}
	r2, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r2.Revision != 1 || r2.Generation != 2 {
		t.Fatal(e, r2)
	}
	if len(r2.Changed) != 0 {
		t.Fatal(r2.Changed)
	}
	r3, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if e != nil || r3.Revision != 2 {
		t.Fatal(e, r3)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	r, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	r, e = s.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
	if snap := s.Snapshot(); snap.Generation != 1 || snap.NextRevision != 2 {
		t.Fatal(snap)
	}
}

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	b := s.Snapshot()
	// Record count exceeded only at batch end.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e, s.Snapshot())
	}
	// Total value bytes exceeded only at batch end.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}, {Put, "a", []byte("567")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e, s.Snapshot())
	}
	// Interim overflow that resolves by batch end is fine.
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}, {Put, "b", []byte("1234")}, {Delete, "a", nil}}})
	if e != nil || len(s.Snapshot().Records) != 1 {
		t.Fatal(e, r)
	}
}

func TestOverwriteAccounting(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}}}); e != nil {
		t.Fatal(e)
	}
	// Replacing same-size value stays within total budget.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("abcd")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("abcde")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("x")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestGetErrorsAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); e != nil || ok {
		t.Fatal(ok, e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); e != nil {
		t.Fatal(e)
	}
	r, ok, e := s.Get("a")
	if e != nil || !ok || r.Revision != 1 || string(r.Value) != "xy" {
		t.Fatal(r, ok, e)
	}
	r.Value[0] = 'Q'
	r2, _, _ := s.Get("a")
	if string(r2.Value) != "xy" {
		t.Fatal("Get must return a copy")
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	if e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'Q'
	if r, _, _ := s.Get("a"); string(r.Value) != "2" {
		t.Fatal("Snapshot must be isolated from the store")
	}
}

func TestChangedDeduplicatedAndSorted(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")},
	}})
	if e != nil || len(r.Changed) != 2 {
		t.Fatal(e, r.Changed)
	}
	if r.Changed[0].Name != "a" || r.Changed[1].Name != "b" || r.Changed[1].Revision != 3 {
		t.Fatal(r.Changed)
	}
	// Put then Delete in one batch: name absent from Changed and store.
	r, e = s.Apply(Batch{Ops: []Op{{Put, "z", []byte("1")}, {Delete, "z", nil}}})
	if e != nil || len(r.Changed) != 0 {
		t.Fatal(e, r.Changed)
	}
	if _, ok, _ := s.Get("z"); ok {
		t.Fatal("z should be deleted")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 {
		t.Fatal(len(snap.Records))
	}
	// Generation counts every successful non-empty batch: 2 per iteration.
	if snap.Generation != 32*20*2 {
		t.Fatal(snap.Generation)
	}
	// Revision only advances on Put.
	if snap.NextRevision != 32*20+1 {
		t.Fatal(snap.NextRevision)
	}
}
