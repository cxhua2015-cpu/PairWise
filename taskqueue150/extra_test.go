package taskqueue150

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongid", "中文"}
	for _, id := range bad {
		if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "ok_id-1", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownKindAndMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Now: 1})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	r, err = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
}

func TestDuplicateAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if len(s.Items) != 0 || s.NextRevision != 1 || s.Generation != 0 {
		t.Fatal(s)
	}
}

func TestPopOrderingAndAtomicity(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 9},
		{Enqueue, "c", 5, 1},
		{Enqueue, "d", 5, 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Pop(5, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"c", "d", "a"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatal(got)
		}
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
}

func TestPolicyValidation(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(1, []string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(1, []string{"a", "a"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, err := NewPolicy(2, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 3); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("b", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 2); err != nil {
		t.Fatal(err)
	}
	if err := p.ReplaceActors(nil); err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}

func TestCoordinatorEngineFailureAudited(t *testing.T) {
	core, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	p, _ := NewPolicy(4, []string{"a"})
	c, err := NewCoordinator(core, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCoordinator(nil, p); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	good := Batch{Ops: []Op{{Enqueue, "x", 1, 0}}}
	if _, err := c.Apply("a", good); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply("a", good); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	d := c.Decisions()
	if len(d) != 2 || d[0].Sequence != 1 || d[1].Sequence != 2 || d[1].Committed || d[1].Error == "" {
		t.Fatal(d)
	}
}

func TestConcurrentAllLayers(t *testing.T) {
	core, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	p, _ := NewPolicy(2, []string{"alice", "bob"})
	c, _ := NewCoordinator(core, p)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := "alice"
			if i%3 == 0 {
				actor = "mallory"
			}
			id := fmt.Sprintf("task-%d", i)
			_, _ = c.Apply(actor, Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_ = c.Decisions()
			_ = core.Snapshot()
			if i%4 == 0 {
				_ = p.ReplaceActors([]string{"alice", "bob"})
			}
		}()
	}
	wg.Wait()
	for _, d := range c.Decisions() {
		if d.Actor == "mallory" && d.Committed {
			t.Fatal("denied actor committed")
		}
	}
	seq := c.Decisions()
	for i, d := range seq {
		if d.Sequence != uint64(i+1) {
			t.Fatal("non-contiguous audit sequence")
		}
	}
	committed := 0
	for _, d := range seq {
		if d.Committed {
			committed++
		}
	}
	if got := len(core.Snapshot().Items); got != committed {
		t.Fatalf("items=%d committed=%d", got, committed)
	}
}

func TestCoordinatorConcurrentDecisionsIsolated(t *testing.T) {
	core, _ := New(Options{MaxItems: 64, MaxIDBytes: 8})
	p, _ := NewPolicy(1, []string{"a"})
	c, _ := NewCoordinator(core, p)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Apply("a", Batch{Ops: []Op{{Enqueue, fmt.Sprintf("i%d", i), 1, 0}}})
		}()
	}
	wg.Wait()
	d := c.Decisions()
	if len(d) != 8 {
		t.Fatal(len(d))
	}
	d[3].Actor = "hacked"
	if !reflect.DeepEqual(c.Decisions()[3].Actor, "a") {
		t.Fatal("decisions alias internal state")
	}
}
