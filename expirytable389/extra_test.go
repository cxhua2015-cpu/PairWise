package expirytable389

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey", "caf\u00e9"} {
		_, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndNegativeValues(t *testing.T) {
	x := table(t)
	for _, b := range []Batch{
		{Ops: []Op{{Kind(0), "a", 1}}},
		{Ops: []Op{{Kind(9), "a", 1}}},
		{Ops: []Op{{Put, "a", -1}}},
		{Now: -1, Ops: []Op{{Put, "a", 1}}},
	} {
		if _, e := x.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", b, e)
		}
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e) // equal time is allowed
	}
}

func TestCandidateExpiryAndRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	// "old" expires at Now=2, freeing capacity for "new".
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "old", 2}, {Put, "kept", 9}}}); e != nil {
		t.Fatal(e)
	}
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "new", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "kept" || s.Entries[1].Key != "new" {
		t.Fatal(s.Entries)
	}
	// Failed batch must roll back the candidate eviction of "gone".
	if _, e := x.Apply(Batch{Now: 10, Ops: []Op{{Put, "x", 1}, {Put, "y", 1}, {Put, "z", 1}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s = x.Snapshot()
	if len(s.Entries) != 2 || s.Now != 2 || s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Revision allocated by an earlier op is rolled back with the batch.
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "b", 9}, {Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.NextRevision != 2 || len(s.Entries) != 1 || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.Generation != 0 || len(s.Entries) != 0 {
		t.Fatal(s)
	}
}

func TestExpireBoundaryAndOrder(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}, {Put, "c", 5}}})
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	if s := x.Snapshot(); s.Now != 4 || len(s.Entries) != 1 || s.Entries[0].Key != "c" {
		t.Fatal(s)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 5}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mut"
	s.Entries[0].ExpiresAt = 99
	again := x.Snapshot()
	if again.Entries[0].Key != "a" || again.Entries[0].ExpiresAt != 5 {
		t.Fatal(again.Entries)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{
					{Put, k, n + 100},
					{Touch, k, n + 200},
				}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 32 || s.Generation == 0 {
		t.Fatal(len(s.Entries), s.Generation)
	}
}
