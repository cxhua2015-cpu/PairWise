package taskqueue145

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptionsAndInput(t *testing.T) {
	if _, e := New(Options{MaxItems: 0, MaxIDBytes: 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxItems: 1, MaxIDBytes: 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},
		{Ops: []Op{{Kind: Enqueue, ID: "Upper"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "toolongidxx"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}},
	}
	for i, b := range bad {
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatal(s)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
}

func TestExistsCapacityAndRevision(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	r1, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil || r1.Revision != 1 || r1.Generation != 1 {
		t.Fatal(r1, e)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 1 || s.NextRevision != 2 || s.Generation != 1 {
		t.Fatal(s)
	}
	if _, e = q.Apply(Batch{Ops: nil}); e != nil || q.Snapshot().Generation != 1 {
		t.Fatal(e)
	}
}

func TestPopOrderAndSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	items, e := q.Pop(10, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "d", "b", "a"}
	for i, id := range want {
		if items[i].ID != id {
			t.Fatal(items)
		}
	}
	if len(q.Snapshot().Items) != 0 {
		t.Fatal("pop did not delete")
	}
	_, _ = q.Apply(Batch{Now: 10, Ops: []Op{{Enqueue, "x", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "x" {
		t.Fatal("snapshot aliases internal state")
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
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
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
				_, _ = coord.Apply(actor, Batch{Now: int64(i), Ops: []Op{{Enqueue, fmt.Sprintf("a%d-%d", g, i), 1, 0}}})
				if i%7 == 0 {
					_ = policy.ReplaceActors([]string{"alice", "carol"})
				}
				_ = coord.Decisions()
			}
		}()
	}
	w.Wait()
	ds := coord.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("gap at %d: %d", i, d.Sequence)
		}
	}
}

func TestCoordinatorEngineFailureAudited(t *testing.T) {
	core, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	policy, _ := NewPolicy(4, []string{"alice"})
	coord, _ := NewCoordinator(core, policy)
	if _, e := coord.Apply("alice", Batch{Ops: []Op{{Kind: 9, ID: "x"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	ds := coord.Decisions()
	if len(ds) != 1 || ds[0].Committed || ds[0].Error == "" {
		t.Fatal(ds)
	}
	if _, e := NewCoordinator(nil, policy); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := NewPolicy(0, nil); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
}
