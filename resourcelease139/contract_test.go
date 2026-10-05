package resourcelease139

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func table(t *testing.T) *Table {
	t.Helper()
	x, e := New(Options{MaxEntries: 3, MaxKeyBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	return x
}
func TestBoundaryAndTouch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Touch, "a", 5}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
}
func TestRollbackTime(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 8}}})
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "bad?", 9}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal(e)
	}
}
func TestFinalCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 2}}})
	_, e := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}, {Put, "b", 3}}})
	if e != nil || x.Snapshot().Entries[0].Key != "b" {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	x, _ := New(Options{MaxEntries: 64, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			_, _ = x.Apply(Batch{Ops: []Op{{Put, k, 9}}})
			_ = x.Snapshot()
		}()
	}
	w.Wait()
	if len(x.Snapshot().Entries) != 20 {
		t.Fatal(len(x.Snapshot().Entries))
	}
}

// Cross-file contract: core state, policy admission and coordinator audit must work together.
func TestPolicyCoordinatorIntegration(t *testing.T) {
	core, err := New(Options{MaxEntries: 8, MaxKeyBytes: 16})
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
	good := Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "coord-a", ExpiresAt: 10}}}
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
