package resourcelease109

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a/b"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "b", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExpireBoundaryClosed(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 5}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 0 {
		t.Fatal(e, gone)
	}
	gone, e = x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].ExpiresAt != 5 {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
}

func TestExpireTimeMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Expire(-1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(3); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(2); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestEvictionOnCandidateAndRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "old", 1}, {Put, "keep", 100}}}); e != nil {
		t.Fatal(e)
	}
	// At Now=2 "old" is evicted on the candidate, freeing capacity for "new".
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "new", 50}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "keep" || s.Entries[1].Key != "new" {
		t.Fatal(s.Entries)
	}
	// A failing batch must roll back evictions, time and revision.
	before := x.Snapshot()
	_, e = x.Apply(Batch{Now: 200, Ops: []Op{{Put, "x1", 300}, {Put, "x2", 300}, {Put, "x3", 300}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if after := x.Snapshot(); after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != len(before.Entries) {
		t.Fatalf("rolled back state mismatch: %+v vs %+v", before, after)
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Put allocates a revision before the failing Touch; revision must roll back.
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.NextRevision != 1 || len(s.Entries) != 0 || s.Generation != 0 {
		t.Fatal(s)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.Generation != 0 || s.Now != 0 {
		t.Fatal(s)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	s.Entries[0].ExpiresAt = 0
	if got := x.Snapshot().Entries[0]; got.Key != "a" || got.ExpiresAt != 9 {
		t.Fatal(got)
	}
	gone, e := x.Expire(9)
	if e != nil || len(gone) != 1 {
		t.Fatal(e)
	}
	gone[0].Key = "mutated"
	if len(x.Snapshot().Entries) != 0 {
		t.Fatal("expire did not remove entry")
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
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 100}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 32 || s.Generation == 0 {
		t.Fatal(s.Generation, len(s.Entries))
	}
}
