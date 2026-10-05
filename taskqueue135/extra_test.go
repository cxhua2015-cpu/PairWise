package taskqueue135

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsAndInputValidation(t *testing.T) {
	if _, e := New(Options{MaxItems: 0, MaxIDBytes: 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxItems: 1, MaxIDBytes: -1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	q := queue(t)
	bad := []Op{
		{Kind: 0, ID: "a"},
		{Kind: 99, ID: "a"},
		{Enqueue, "", 0, 0},
		{Enqueue, "Upper", 0, 0},
		{Enqueue, "toolongid", 0, 0},
		{Enqueue, "bad char", 0, 0},
	}
	for _, op := range bad {
		if _, e := q.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTimeAndGeneration(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	r, e := q.Apply(Batch{Now: 5})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	if s := q.Snapshot(); s.Generation != 1 || s.Now != 5 {
		t.Fatal(s)
	}
}

func TestDuplicateAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 0 || s.NextRevision != 1 || s.Generation != 0 {
		t.Fatal(s)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "ghost", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestRevisionMonotonicAcrossBatches(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	r2, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if r1.Revision != 1 || r2.Revision != 2 {
		t.Fatal(r1, r2)
	}
	if s := q.Snapshot(); s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestPopOrderingAndAtomicDelete(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 0},
		{Enqueue, "y", 5, 1},
		{Enqueue, "z", 5, 0},
		{Enqueue, "w", 5, 0},
	}})
	got, e := q.Pop(10, 3)
	if e != nil || len(got) != 3 || got[0].ID != "w" || got[1].ID != "z" || got[2].ID != "y" {
		t.Fatal(e, got)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal(q.Snapshot())
	}
	// ReadyAt gate: nothing ready yet.
	q2 := queue(t)
	_, _ = q2.Apply(Batch{Ops: []Op{{Enqueue, "later", 9, 100}}})
	if got, _ := q2.Pop(50, 1); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestConcurrentApplyPop(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-i%d", g, i)
				_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
}

func TestConcurrentPolicyCoordinator(t *testing.T) {
	core, _ := New(Options{MaxItems: 1024, MaxIDBytes: 16})
	policy, _ := NewPolicy(2, []string{"alice"})
	coord, _ := NewCoordinator(core, policy)
	var w sync.WaitGroup
	for g := 0; g < 6; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			actor := "alice"
			if g%2 == 1 {
				actor = "mallory"
			}
			for i := 0; i < 40; i++ {
				_, _ = coord.Apply(actor, Batch{Ops: []Op{{Enqueue, fmt.Sprintf("a%d-%d", g, i), i, 0}}})
				_ = coord.Decisions()
				if i%10 == 0 {
					_ = policy.ReplaceActors([]string{"alice", "carol"})
				}
			}
		}()
	}
	w.Wait()
	ds := coord.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("sequence gap at %d: %+v", i, d)
		}
		if !d.Committed && d.Error == "" {
			t.Fatalf("rejected decision missing error: %+v", d)
		}
	}
}

func TestCoordinatorEngineFailureAudited(t *testing.T) {
	core, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	policy, _ := NewPolicy(4, []string{"alice"})
	coord, _ := NewCoordinator(core, policy)
	if _, e := NewCoordinator(nil, policy); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := coord.Apply("alice", Batch{Ops: []Op{{Cancel, "ghost", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	ds := coord.Decisions()
	if len(ds) != 1 || ds[0].Committed || ds[0].Error != ErrNotFound.Error() {
		t.Fatal(ds)
	}
}
