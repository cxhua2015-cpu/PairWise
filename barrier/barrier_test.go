package barrier

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestCrossGenerationSameBatch(t *testing.T) {
	r, _ := New(Options{MaxBarriers: 1, MaxPending: 8, MaxNameBytes: 8}, []Config{{Name: "b", Parties: 2}})
	x, e := r.Apply(Batch{Ops: []Op{
		arr("b", "x"), arr("b", "y"), // completes gen 1
		arr("b", "x"), arr("b", "z"), // completes gen 2
		arr("b", "x"), // pending in gen 3
	}})
	if e != nil || len(x.Completions) != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if x.Completions[0].Generation != 1 || x.Completions[1].Generation != 2 {
		t.Fatalf("x=%+v", x)
	}
	if !reflect.DeepEqual(x.Completions[1].Participants, []string{"x", "z"}) {
		t.Fatalf("x=%+v", x)
	}
	s := r.Snapshot()
	if s.Barriers[0].Generation != 3 || !reflect.DeepEqual(s.Barriers[0].Pending, []string{"x"}) {
		t.Fatalf("s=%+v", s)
	}
}

func TestCancelWithinBatch(t *testing.T) {
	r, _ := New(Options{MaxBarriers: 1, MaxPending: 8, MaxNameBytes: 8}, []Config{{Name: "b", Parties: 2}})
	x, e := r.Apply(Batch{Ops: []Op{arr("b", "a"), cancel("b", "a"), arr("b", "a"), arr("b", "c")}})
	if e != nil || len(x.Completions) != 1 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if !reflect.DeepEqual(x.Completions[0].Participants, []string{"a", "c"}) {
		t.Fatalf("x=%+v", x)
	}
	if r.Snapshot().Pending != 0 {
		t.Fatalf("s=%+v", r.Snapshot())
	}
}

func TestDuplicateAcrossGenerationsAllowed(t *testing.T) {
	r, _ := New(Options{MaxBarriers: 1, MaxPending: 8, MaxNameBytes: 8}, []Config{{Name: "b", Parties: 1}})
	x, e := r.Apply(Batch{Ops: []Op{arr("b", "a"), arr("b", "a")}})
	if e != nil || len(x.Completions) != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if r.Snapshot().Barriers[0].Generation != 3 {
		t.Fatalf("s=%+v", r.Snapshot())
	}
}

func TestDuplicateConflictRollsBackCompletion(t *testing.T) {
	r, _ := New(Options{MaxBarriers: 1, MaxPending: 8, MaxNameBytes: 8}, []Config{{Name: "b", Parties: 3}})
	before := r.Snapshot()
	_, e := r.Apply(Batch{Ops: []Op{arr("b", "a"), arr("b", "c"), arr("b", "a")}})
	if !errors.Is(e, ErrConflict) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("leaked: %+v", r.Snapshot())
	}
}

func TestCapacityRollsBackCompletions(t *testing.T) {
	r, _ := New(Options{MaxBarriers: 2, MaxPending: 1, MaxNameBytes: 8}, []Config{{Name: "a", Parties: 2}, {Name: "b", Parties: 5}})
	before := r.Snapshot()
	// completes barrier a (net 0), then leaves 2 pending on b -> exceeds 1.
	_, e := r.Apply(Batch{Ops: []Op{arr("a", "x"), arr("a", "y"), arr("b", "p"), arr("b", "q")}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("leaked: %+v", r.Snapshot())
	}
}

func TestCrossBarrierIsolation(t *testing.T) {
	r, _ := New(Options{MaxBarriers: 2, MaxPending: 8, MaxNameBytes: 8}, []Config{{Name: "a", Parties: 2}, {Name: "b", Parties: 2}})
	x, e := r.Apply(Batch{Ops: []Op{arr("a", "p"), arr("b", "p")}})
	if e != nil || len(x.Completions) != 0 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	s := r.Snapshot()
	if s.Pending != 2 || s.Barriers[0].Generation != 1 || s.Barriers[1].Generation != 1 {
		t.Fatalf("s=%+v", s)
	}
}

func TestEmptyBatchNoGenerationBump(t *testing.T) {
	r, _ := New(Options{MaxBarriers: 1, MaxPending: 4, MaxNameBytes: 8}, []Config{{Name: "a", Parties: 2}})
	x, e := r.Apply(Batch{})
	if e != nil || x.Generation != 0 || len(x.Completions) != 0 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	x, _ = r.Apply(Batch{Ops: []Op{arr("a", "p")}})
	if x.Generation != 1 {
		t.Fatalf("x=%+v", x)
	}
}

func TestStructuralValidationPrecedesSemantics(t *testing.T) {
	r, _ := New(Options{MaxBarriers: 1, MaxPending: 4, MaxNameBytes: 8}, []Config{{Name: "a", Parties: 2}})
	// Unknown barrier first, invalid op later: structural error wins.
	_, e := r.Apply(Batch{Ops: []Op{arr("nope", "p"), {Kind: 0, Barrier: "a", Participant: "p"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
	for _, bad := range []Op{
		{Kind: Arrive, Barrier: "", Participant: "p"},
		{Kind: Arrive, Barrier: "a", Participant: ""},
		{Kind: Arrive, Barrier: "a", Participant: "bad?"},
		{Kind: Arrive, Barrier: "a", Participant: "toolongname"},
		{Kind: Cancel, Barrier: "a b", Participant: "p"},
	} {
		if _, e := r.Apply(Batch{Ops: []Op{bad}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op=%+v e=%v", bad, e)
		}
	}
}

func TestCompletionOwnershipIsolation(t *testing.T) {
	r, _ := New(Options{MaxBarriers: 1, MaxPending: 8, MaxNameBytes: 8}, []Config{{Name: "a", Parties: 2}})
	x, _ := r.Apply(Batch{Ops: []Op{arr("a", "x"), arr("a", "y")}})
	x.Completions[0].Participants[0] = "mutated"
	s := r.Snapshot()
	if s.Barriers[0].Generation != 2 {
		t.Fatalf("s=%+v", s)
	}
	x2, _ := r.Apply(Batch{Ops: []Op{arr("a", "x"), arr("a", "y")}})
	if !reflect.DeepEqual(x2.Completions[0].Participants, []string{"x", "y"}) {
		t.Fatalf("x2=%+v", x2)
	}
}

func TestConcurrentMixedOps(t *testing.T) {
	r, _ := New(Options{MaxBarriers: 2, MaxPending: 256, MaxNameBytes: 16}, []Config{{Name: "a", Parties: 3}, {Name: "b", Parties: 3}})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := fmt.Sprintf("p-%02d", i)
			_, _ = r.Apply(Batch{Ops: []Op{arr("a", p), arr("b", p)}})
			_, _ = r.Apply(Batch{Ops: []Op{cancel("a", p), cancel("b", p)}})
			_ = r.Snapshot()
		}()
	}
	wg.Wait()
	s := r.Snapshot()
	total := 0
	for _, b := range s.Barriers {
		total += len(b.Pending)
	}
	if total != s.Pending {
		t.Fatalf("inconsistent: %+v", s)
	}
	if s.Generation > 64 {
		t.Fatalf("gen=%d", s.Generation)
	}
}
