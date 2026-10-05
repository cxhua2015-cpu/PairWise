package resourcelease149

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptionsAndInput(t *testing.T) {
	if _, e := New(Options{MaxEntries: 0, MaxKeyBytes: 8}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxEntries: 1, MaxKeyBytes: 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x := table(t)
	for _, b := range []Batch{
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Bad", 1}}},
		{Ops: []Op{{Put, "toolongkey", 1}}},
		{Ops: []Op{{Kind(9), "a", 1}}},
		{Now: -1},
	} {
		if _, e := x.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	if s := x.Snapshot(); s.Generation != 0 || len(s.Entries) != 0 {
		t.Fatal(s)
	}
}

func TestNotFoundAndCapacityRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}, {Put, "b", 100}, {Put, "c", 100}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "d", 100}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Generation != before.Generation || after.Now != before.Now || after.NextRevision != before.NextRevision || len(after.Entries) != 3 {
		t.Fatalf("rollback violated: %+v -> %+v", before, after)
	}
}

func TestExpiryOnApplyAndEmptyBatch(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 5}}}); e != nil {
		t.Fatal(e)
	}
	g := x.Snapshot().Generation
	r, e := x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != g {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" || s.Now != 2 {
		t.Fatal(s)
	}
	if _, e := x.Expire(1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestConcurrentCoordinator(t *testing.T) {
	core, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	policy, _ := NewPolicy(2, []string{"alice", "bob"})
	coord, _ := NewCoordinator(core, policy)
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			actor := "alice"
			if i%3 == 0 {
				actor = "mallory"
			}
			if i%7 == 0 {
				_ = policy.ReplaceActors([]string{"alice", "bob"})
			}
			_, _ = coord.Apply(actor, Batch{Now: int64(i), Ops: []Op{{Put, fmt.Sprintf("k-%d", i), 1000}}})
			_ = coord.Decisions()
			_ = core.Snapshot()
			_, _ = core.Expire(int64(i))
		}()
	}
	w.Wait()
	ds := coord.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("gap in audit sequence at %d: %+v", i, d)
		}
	}
}
