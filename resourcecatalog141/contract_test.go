package resourcecatalog141

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func store(t *testing.T) *Store {
	t.Helper()
	s, e := New(Options{MaxRecords: 3, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestOrderRevision(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "a", []byte("3")}}})
	if e != nil || x.Revision != 3 || len(x.Changed) != 2 || x.Changed[0].Name != "a" || x.Changed[0].Revision != 3 {
		t.Fatal(e, x)
	}
}
func TestRollback(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Delete, "z", nil}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}
func TestFinalCapacityOwnership(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	v := []byte("abc")
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", v}}})
	v[0] = 'z'
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("zz")}}})
	r, _, _ := s.Get("b")
	r.Value[0] = 'q'
	r2, _, _ := s.Get("b")
	if e != nil || string(r2.Value) != "zz" {
		t.Fatal(e)
	}
}
func TestValidation(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "bad?", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 128})
	var w sync.WaitGroup
	for i := 0; i < 24; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			_, _, _ = s.Get(k)
			_ = s.Snapshot()
		}()
	}
	w.Wait()
	if len(s.Snapshot().Records) != 24 {
		t.Fatal(len(s.Snapshot().Records))
	}
}

// Cross-file contract: core state, policy admission and coordinator audit must work together.
func TestPolicyCoordinatorIntegration(t *testing.T) {
	core, err := New(Options{MaxRecords: 8, MaxNameBytes: 16, MaxValueBytes: 16, MaxTotalValueBytes: 64})
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
	good := Batch{Ops: []Op{{Kind: Put, Name: "coord-a", Value: []byte("v")}}}
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
