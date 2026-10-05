package taskqueue140

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

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},
		{Ops: []Op{{Kind: Enqueue, ID: "Upper"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "has space"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "toolongidxx"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}},
	}
	for i, b := range bad {
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatalf("failed validation mutated state: %+v", s)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 || len(s.Items) != 1 {
		t.Fatalf("time rollback mutated state: %+v", s)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	// Empty batch: generation unchanged.
	r, e = q.Apply(Batch{})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	// Failed batch: revision not consumed.
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Cancel, "missing", 0, 0}}})
	if s := q.Snapshot(); s.NextRevision != 3 || s.Generation != 1 {
		t.Fatalf("rollback leaked revision/generation: %+v", s)
	}
	r, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if e != nil || r.Revision != 3 {
		t.Fatal(r, e)
	}
}

func TestExistsNotFoundCapacity(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Final capacity only: cancel-then-enqueue over the limit mid-batch is fine.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("capacity failure must roll back")
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestPopOrderAndAtomicDelete(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "low", 1, 0},
		{Enqueue, "hi2", 5, 2},
		{Enqueue, "hi1", 5, 1},
		{Enqueue, "future", 9, 100},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(10, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"hi1", "hi2", "low"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatal(got)
		}
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal("pop must delete atomically, remaining:", n)
	}
	if _, e := q.Pop(10, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "a" {
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
				id := fmt.Sprintf("g%d-item%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal("capacity violated")
	}
}

func TestPolicyBoundaries(t *testing.T) {
	if _, e := NewPolicy(0, nil); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := NewPolicy(1, []string{"Bad Name"}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	p, e := NewPolicy(2, []string{"alice"})
	if e != nil {
		t.Fatal(e)
	}
	if e := p.Authorize("alice", 2); e != nil {
		t.Fatal(e)
	}
	if e := p.Authorize("alice", 3); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.Authorize("mallory", 1); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.ReplaceActors([]string{"bad actor"}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Failed replacement keeps the old list.
	if e := p.Authorize("alice", 1); e != nil {
		t.Fatal(e)
	}
}

func TestCoordinatorEngineFailureAudit(t *testing.T) {
	core, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	p, _ := NewPolicy(4, []string{"alice"})
	c, e := NewCoordinator(core, p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = NewCoordinator(nil, p); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e = NewCoordinator(core, nil); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	good := Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}
	if _, e = c.Apply("alice", good); e != nil {
		t.Fatal(e)
	}
	// Engine failure (duplicate ID) is audited but not committed.
	if _, e = c.Apply("alice", good); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	d := c.Decisions()
	if len(d) != 2 || d[0].Sequence != 1 || d[1].Sequence != 2 ||
		!d[0].Committed || d[1].Committed || d[1].Error == "" {
		t.Fatal(d)
	}
}

func TestConcurrentCoordinator(t *testing.T) {
	core, _ := New(Options{MaxItems: 1024, MaxIDBytes: 16})
	p, _ := NewPolicy(1, []string{"alice", "bob"})
	c, _ := NewCoordinator(core, p)
	var w sync.WaitGroup
	for g := 0; g < 16; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			actor := "alice"
			if g%3 == 0 {
				actor = "mallory"
			}
			for i := 0; i < 25; i++ {
				_, _ = c.Apply(actor, Batch{Ops: []Op{{Enqueue, fmt.Sprintf("g%d-i%d", g, i), 1, 0}}})
				if g == 0 && i == 12 {
					_ = p.ReplaceActors([]string{"alice", "bob", "carol"})
				}
			}
		}()
	}
	w.Wait()
	d := c.Decisions()
	if len(d) != 16*25 {
		t.Fatal("every attempt must be audited:", len(d))
	}
	for i, dec := range d {
		if dec.Sequence != uint64(i+1) {
			t.Fatal("audit sequence not contiguous:", i, dec.Sequence)
		}
	}
	d[0].Actor = "mutated"
	if c.Decisions()[0].Actor == "mutated" {
		t.Fatal("decision slice aliases internal state")
	}
}
