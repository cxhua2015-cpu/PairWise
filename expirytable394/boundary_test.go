package expirytable394

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestStructuralBeforeTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	// Unknown kind wins over time regression.
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Kind(9), "a", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Invalid key wins over time regression.
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "Bad", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Oversized key.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "123456789", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Empty key and negative times.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Pure time regression after valid structure.
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestEvictionClosedBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	// "a" (ExpiresAt 5 <= Now 5) evicted on the next Apply candidate.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatal(s.Entries)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}, {Put, "b", 100}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Final state would exceed capacity: everything rolls back.
	_, e := x.Apply(Batch{Now: 4, Ops: []Op{{Delete, "a", 0}, {Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Entries) != 2 {
		t.Fatalf("not rolled back: %+v -> %+v", before, after)
	}
	// NotFound also rolls back evictions and revisions.
	_, e = x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "missing", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != 1 || len(got.Entries) != 2 {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	if x.Snapshot().NextRevision != 3 {
		t.Fatal(x.Snapshot())
	}
}

func TestExpireMonotonicAndOwnership(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 4}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = -1
	if x.Snapshot().Entries[0].ExpiresAt != 9 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 256, MaxKeyBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
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
		t.Fatalf("entries=%d generation=%d", len(s.Entries), s.Generation)
	}
}
