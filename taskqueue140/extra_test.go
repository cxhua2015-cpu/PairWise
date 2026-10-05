package taskqueue140

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestInvalidOps(t *testing.T) {
	q := queue(t)
	bad := []Op{
		{Kind: 0, ID: "a"},
		{Kind: 99, ID: "a"},
		{Kind: Enqueue, ID: ""},
		{Kind: Enqueue, ID: "Upper"},
		{Kind: Enqueue, ID: "with space"},
		{Kind: Enqueue, ID: "toolongid"}, // > MaxIDBytes=8
		{Kind: Enqueue, ID: "a", ReadyAt: -1},
	}
	for _, op := range bad {
		if _, err := q.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	if _, err := q.Apply(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if got := q.Snapshot().Generation; got != 0 {
		t.Fatal(got)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
}

func TestExistsNotFoundCapacity(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Final capacity check only at the end: enqueue over limit fails,
	// but cancel+enqueue within one batch succeeds.
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if len(s.Items) != 1 || s.Items[0].ID != "b" || s.Generation != 2 {
		t.Fatal(s)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if err != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, err)
	}
	// Failed batch must not consume revisions or bump generation.
	if _, err = q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r, err = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if err != nil || r.Generation != 2 || r.Revision != 3 {
		t.Fatal(r, err)
	}
	// Empty batch leaves generation unchanged.
	r, err = q.Apply(Batch{})
	if err != nil || r.Generation != 2 {
		t.Fatal(r, err)
	}
	s := q.Snapshot()
	if s.NextRevision != 4 || s.Generation != 2 {
		t.Fatal(s)
	}
	// Revisions are assigned in op order.
	if s.Items[0].Revision != 1 || s.Items[1].Revision != 2 || s.Items[2].Revision != 3 {
		t.Fatal(s.Items)
	}
}

func TestPopOrderAndLimit(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 1},
		{Enqueue, "b", 3, 2},
		{Enqueue, "c", 3, 1},
		{Enqueue, "d", 3, 1},
		{Enqueue, "e", 2, 9}, // not ready
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Pop(5, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"c", "d", "b"} // priority desc, readyAt asc, id asc
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	// "e" is not ready yet; only "a" remains eligible.
	got, err = q.Pop(5, 10)
	if err != nil || len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got, err)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
	if _, err := q.Pop(0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
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
	p, _ := q.Pop(0, 1)
	p[0].ID = "mutated"
	if q.Snapshot().NextRevision != 2 {
		t.Fatal("pop result aliases internal state")
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
				id := fmt.Sprintf("g%d-%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(1, []string{"Bad Name"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, err := NewPolicy(2, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("alice", 3); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("mallory", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.ReplaceActors([]string{"bad actor"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Failed replacement keeps the old list.
	if err := p.Authorize("alice", 1); err != nil {
		t.Fatal(err)
	}
}

func TestCoordinatorNilArgs(t *testing.T) {
	q := queue(t)
	p, _ := NewPolicy(1, []string{"a"})
	if _, err := NewCoordinator(nil, p); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := NewCoordinator(q, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestCoordinatorEngineFailureAudit(t *testing.T) {
	q := queue(t)
	p, _ := NewPolicy(4, []string{"alice"})
	c, _ := NewCoordinator(q, p)
	bad := Batch{Ops: []Op{{Kind: 0, ID: "x"}}}
	if _, err := c.Apply("alice", bad); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := c.Apply("alice", Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	d := c.Decisions()
	if len(d) != 2 || d[0].Sequence != 1 || d[0].Committed || d[0].Error == "" ||
		d[1].Sequence != 2 || !d[1].Committed || d[1].Error != "" {
		t.Fatal(d)
	}
}

func TestConcurrentCoordinator(t *testing.T) {
	q, _ := New(Options{MaxItems: 1024, MaxIDBytes: 16})
	p, _ := NewPolicy(1, []string{"alice", "bob"})
	c, _ := NewCoordinator(q, p)
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			actor := "alice"
			if g%2 == 0 {
				actor = "mallory"
			}
			for i := 0; i < 50; i++ {
				_, _ = c.Apply(actor, Batch{Now: int64(i), Ops: []Op{{Enqueue, fmt.Sprintf("x%d-%d", g, i), 1, 0}}})
				if g == 1 {
					_ = p.ReplaceActors([]string{"alice", "bob"})
				}
				_ = c.Decisions()
			}
		}()
	}
	w.Wait()
	d := c.Decisions()
	for i, dec := range d {
		if dec.Sequence != uint64(i+1) {
			t.Fatalf("gap in audit sequence at %d: %v", i, dec)
		}
	}
}
