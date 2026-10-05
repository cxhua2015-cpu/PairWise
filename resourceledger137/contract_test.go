package resourceledger137

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func led(t *testing.T) *Ledger {
	t.Helper()
	l, e := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 20})
	if e != nil {
		t.Fatal(e)
	}
	return l
}
func TestOrder(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{{Add, "a", 3, 0}, {Add, "a", 2, 0}, {Set, "b", 0, 5}}})
	if e != nil || x.Revision != 3 || len(x.Changed) != 2 {
		t.Fatal(e, x)
	}
	top, _ := l.Top(2)
	if top[0].Name != "a" || top[1].Name != "b" {
		t.Fatal(top)
	}
}
func TestRollback(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	b := l.Snapshot()
	_, e := l.Apply(Batch{Ops: []Op{{Add, "a", 2, 0}, {Delete, "z", 0, 0}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, l.Snapshot()) {
		t.Fatal(e)
	}
}
func TestFinalCapacity(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 5})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	_, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "b", 0, -2}}})
	if e != nil || l.Snapshot().Accounts[0].Name != "b" {
		t.Fatal(e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Add, "b", -4, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 100})
	var w sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
			_, _ = l.Top(64)
			_ = l.Snapshot()
		}()
	}
	w.Wait()
	if len(l.Snapshot().Accounts) != 20 {
		t.Fatal(len(l.Snapshot().Accounts))
	}
}

// Cross-file contract: core state, policy admission and coordinator audit must work together.
func TestPolicyCoordinatorIntegration(t *testing.T) {
	core, err := New(Options{MaxAccounts: 8, MaxNameBytes: 16, MaxAbsValue: 100})
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
	good := Batch{Ops: []Op{{Kind: Add, Name: "coord-a", Delta: 1}}}
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
