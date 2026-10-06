package expirytable254

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustTable(t *testing.T, maxEntries, maxKeyBytes int) *Table {
	t.Helper()
	x, err := New(Options{MaxEntries: maxEntries, MaxKeyBytes: maxKeyBytes})
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	x := mustTable(t, 4, 6)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Put, "", 5}}},
		{Ops: []Op{{Put, "Upper", 5}}},
		{Ops: []Op{{Put, "toolong", 5}}},
		{Ops: []Op{{Put, "bad key", 5}}},
		{Ops: []Op{{Kind(0), "a", 5}}},
		{Ops: []Op{{Kind(99), "a", 5}}},
		{Now: 5, Ops: []Op{{Put, "a", 5}}},   // ExpiresAt <= Now
		{Now: 5, Ops: []Op{{Touch, "a", 3}}}, // ExpiresAt <= Now
	}
	for i, b := range bad {
		if err := x.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := x.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := x.ValidateBatch(Batch{Now: 1, Ops: []Op{{Put, "a_1-x", 2}, {Delete, "a_1-x", 0}}}); err != nil {
		t.Fatal(err)
	}
	if s := x.Stats(); s.Entries != 0 || s.Generation != 0 {
		t.Fatalf("failed applies mutated state: %+v", s)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	x := mustTable(t, 4, 6)
	r, err := x.Apply(Batch{Now: 7})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 0 || x.Snapshot().Now != 7 {
		t.Fatalf("empty batch: %+v", r)
	}
	if _, err := x.Apply(Batch{Now: 6}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestCapacityRollback(t *testing.T) {
	x := mustTable(t, 2, 8)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// Sweep would drop "a" (ExpiresAt 2 <= Now 2), but the batch still
	// overflows, so everything including the sweep must roll back.
	_, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision || len(after.Entries) != 2 {
		t.Fatalf("rollback: %+v vs %+v", before, after)
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := mustTable(t, 4, 6)
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "a", 5}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	if _, err := x.Apply(Batch{Ops: []Op{{Touch, "zz", 9}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Delete, "zz", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// A failing batch must not consume revisions from earlier ops.
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "b", 5}, {Touch, "zz", 9}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if got := x.Snapshot(); got.NextRevision != before.NextRevision || len(got.Entries) != 1 {
		t.Fatalf("revision leaked: %+v", got)
	}
}

func TestTouchAndOverwrite(t *testing.T) {
	x := mustTable(t, 4, 6)
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "a", 5}}}); err != nil {
		t.Fatal(err)
	}
	r, err := x.Apply(Batch{Now: 1, Ops: []Op{{Touch, "a", 9}, {Put, "a", 10}}})
	if err != nil || r.Revision != 3 {
		t.Fatal(r, err)
	}
	e := x.Snapshot().Entries[0]
	if e.ExpiresAt != 10 || e.Revision != 3 {
		t.Fatalf("%+v", e)
	}
}

func TestExpireBoundaryAndClock(t *testing.T) {
	x := mustTable(t, 4, 6)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); err != nil {
		t.Fatal(err)
	}
	gone, err := x.Expire(5)
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(gone, err)
	}
	if _, err := x.Expire(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if got := x.Stats(); got.Now != 5 || got.Entries != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestCloneIndependence(t *testing.T) {
	x := mustTable(t, 4, 6)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); err != nil {
		t.Fatal(err)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != x.Stats() {
		t.Fatal("clone diverges at birth")
	}
	if _, err := c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}}); err != nil {
		t.Fatal(err)
	}
	if x.Stats().Entries != 1 || c.Stats().Entries != 0 {
		t.Fatal("clone aliases original")
	}
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().NextRevision == x.Stats().NextRevision {
		t.Fatal("clocks alias")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x := mustTable(t, 128, 16)
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			key := fmt.Sprintf("key-%d", i)
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, key, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				if n%10 == 0 {
					_, _ = x.Expire(n)
					_, _ = x.Clone()
				}
				if err := x.ValidateBatch(Batch{Now: n, Ops: []Op{{Put, key, n + 1}}}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries > 128 || s.NextRevision < 2 {
		t.Fatalf("%+v", s)
	}
}
