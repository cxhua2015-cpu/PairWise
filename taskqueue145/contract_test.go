package taskqueue145

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func queue(t *testing.T) *Queue {
	t.Helper()
	q, e := New(Options{MaxItems: 4, MaxIDBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	return q
}
func TestOrder(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "b", 2, 2}, {Enqueue, "a", 2, 2}, {Enqueue, "c", 3, 3}}})
	if e != nil {
		t.Fatal(e)
	}
	x, e := q.Pop(3, 2)
	if e != nil || x[0].ID != "c" || x[1].ID != "a" {
		t.Fatal(e, x)
	}
}
func TestRollback(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}})
	b := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Cancel, "z", 0, 0}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal(e)
	}
}
func TestFinalCapacityTime(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}})
	_, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "b", 1, 2}}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.Pop(-1, 1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	q, _ := New(Options{MaxItems: 64, MaxIDBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, k, i, 0}}})
			_ = q.Snapshot()
		}()
	}
	w.Wait()
	if len(q.Snapshot().Items) != 20 {
		t.Fatal(len(q.Snapshot().Items))
	}
}

// Cross-file contract: core state, policy admission and coordinator audit must work together.
func TestPolicyCoordinatorIntegration(t *testing.T) {
	core, err := New(Options{MaxItems: 8, MaxIDBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicy(1, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	coord, err := NewCoordinator(core, policy)
	if err != nil {
		t.Fatal(err)
	}
	good := Batch{Now: 1, Ops: []Op{{Kind: Enqueue, ID: "coord-a", Priority: 1, ReadyAt: 1}}}
	before := core.Snapshot()
	if _, err = coord.Apply("bob", good); !errors.Is(err, ErrDenied) || !reflect.DeepEqual(before, core.Snapshot()) {
		t.Fatal(err)
	}
	result, err := coord.Apply("alice", good)
	if err != nil || result.Generation != 1 {
		t.Fatal(err, result)
	}
	tooMany := good
	tooMany.Ops = append(append([]Op(nil), good.Ops...), good.Ops...)
	snapshot := core.Snapshot()
	if _, err = coord.Apply("alice", tooMany); !errors.Is(err, ErrDenied) || !reflect.DeepEqual(snapshot, core.Snapshot()) {
		t.Fatal(err)
	}
	decisions := coord.Decisions()
	if len(decisions) != 3 || decisions[0].Sequence != 1 || decisions[0].Committed || !decisions[1].Committed || decisions[1].Generation != 1 || decisions[2].Sequence != 3 {
		t.Fatal(decisions)
	}
	decisions[0].Actor = "mutated"
	if coord.Decisions()[0].Actor != "bob" {
		t.Fatal("decision slice aliases internal state")
	}
	if err = policy.ReplaceActors([]string{"carol"}); err != nil {
		t.Fatal(err)
	}
	if _, err = coord.Apply("alice", good); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}
