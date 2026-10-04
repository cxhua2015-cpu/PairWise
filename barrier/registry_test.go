package barrier

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func mk(t *testing.T, o Options, cfgs ...Config) *Registry {
	t.Helper()
	r, err := New(o, cfgs)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCrossGenerationSameBatch(t *testing.T) {
	r := mk(t, Options{MaxBarriers: 1, MaxPending: 8, MaxNameBytes: 8}, Config{Name: "b", Parties: 2})
	x, err := r.Apply(Batch{Ops: []Op{
		{Kind: Arrive, Barrier: "b", Participant: "a"},
		{Kind: Arrive, Barrier: "b", Participant: "b"},
		{Kind: Arrive, Barrier: "b", Participant: "a"},
		{Kind: Arrive, Barrier: "b", Participant: "c"},
	}})
	if err != nil || len(x.Completions) != 2 {
		t.Fatalf("x=%+v err=%v", x, err)
	}
	if x.Completions[0].Generation != 1 || x.Completions[1].Generation != 2 {
		t.Fatalf("gens=%v,%v", x.Completions[0].Generation, x.Completions[1].Generation)
	}
	if !reflect.DeepEqual(x.Completions[1].Participants, []string{"a", "c"}) {
		t.Fatalf("p=%v", x.Completions[1].Participants)
	}
	if g := r.Snapshot().Barriers[0].Generation; g != 3 {
		t.Fatalf("gen=%d", g)
	}
}

func TestCancelAcrossGenerations(t *testing.T) {
	r := mk(t, Options{MaxBarriers: 1, MaxPending: 8, MaxNameBytes: 8}, Config{Name: "b", Parties: 2})
	x, err := r.Apply(Batch{Ops: []Op{
		{Kind: Arrive, Barrier: "b", Participant: "a"},
		{Kind: Arrive, Barrier: "b", Participant: "b"},
		{Kind: Arrive, Barrier: "b", Participant: "a"},
		{Kind: Cancel, Barrier: "b", Participant: "a"},
	}})
	if err != nil || len(x.Completions) != 1 || r.Snapshot().Pending != 0 {
		t.Fatalf("x=%+v err=%v s=%+v", x, err, r.Snapshot())
	}
}

func TestDuplicateArriveConflictRollback(t *testing.T) {
	r := mk(t, Options{MaxBarriers: 1, MaxPending: 8, MaxNameBytes: 8}, Config{Name: "b", Parties: 3})
	if _, err := r.Apply(Batch{Ops: []Op{{Kind: Arrive, Barrier: "b", Participant: "a"}}}); err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	_, err := r.Apply(Batch{Ops: []Op{
		{Kind: Arrive, Barrier: "b", Participant: "c"},
		{Kind: Arrive, Barrier: "b", Participant: "a"},
	}})
	if !errors.Is(err, ErrConflict) || !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("err=%v s=%+v", err, r.Snapshot())
	}
}

func TestRollbackDropsCompletionsAndGenerations(t *testing.T) {
	r := mk(t, Options{MaxBarriers: 1, MaxPending: 8, MaxNameBytes: 8}, Config{Name: "b", Parties: 2})
	before := r.Snapshot()
	_, err := r.Apply(Batch{Ops: []Op{
		{Kind: Arrive, Barrier: "b", Participant: "a"},
		{Kind: Arrive, Barrier: "b", Participant: "b"}, // completes gen 1
		{Kind: Cancel, Barrier: "b", Participant: "ghost"},
	}})
	if !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("err=%v s=%+v", err, r.Snapshot())
	}
}

func TestFinalCapacityRollback(t *testing.T) {
	r := mk(t, Options{MaxBarriers: 2, MaxPending: 2, MaxNameBytes: 8},
		Config{Name: "a", Parties: 5}, Config{Name: "b", Parties: 5})
	_, err := r.Apply(Batch{Ops: []Op{
		{Kind: Arrive, Barrier: "a", Participant: "x"},
		{Kind: Arrive, Barrier: "b", Participant: "y"},
		{Kind: Arrive, Barrier: "b", Participant: "z"},
	}})
	if !errors.Is(err, ErrCapacity) || r.Snapshot().Pending != 0 {
		t.Fatalf("err=%v s=%+v", err, r.Snapshot())
	}
	// Intermediate completion frees capacity; final count is what matters.
	r2 := mk(t, Options{MaxBarriers: 1, MaxPending: 1, MaxNameBytes: 8}, Config{Name: "b", Parties: 2})
	if _, err := r2.Apply(Batch{Ops: []Op{
		{Kind: Arrive, Barrier: "b", Participant: "x"},
		{Kind: Arrive, Barrier: "b", Participant: "y"},
		{Kind: Arrive, Barrier: "b", Participant: "z"},
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestCrossBarrierIsolation(t *testing.T) {
	r := mk(t, Options{MaxBarriers: 2, MaxPending: 8, MaxNameBytes: 8},
		Config{Name: "a", Parties: 2}, Config{Name: "b", Parties: 2})
	x, err := r.Apply(Batch{Ops: []Op{
		{Kind: Arrive, Barrier: "a", Participant: "p"},
		{Kind: Arrive, Barrier: "b", Participant: "p"},
	}})
	if err != nil || len(x.Completions) != 0 {
		t.Fatalf("x=%+v err=%v", x, err)
	}
	s := r.Snapshot()
	if s.Barriers[0].Pending[0] != "p" || s.Barriers[1].Pending[0] != "p" {
		t.Fatalf("s=%+v", s)
	}
}

func TestCompletionParticipantSortingAndOrder(t *testing.T) {
	r := mk(t, Options{MaxBarriers: 2, MaxPending: 8, MaxNameBytes: 8},
		Config{Name: "x", Parties: 2}, Config{Name: "y", Parties: 2})
	x, err := r.Apply(Batch{Ops: []Op{
		{Kind: Arrive, Barrier: "y", Participant: "m"},
		{Kind: Arrive, Barrier: "x", Participant: "b"},
		{Kind: Arrive, Barrier: "x", Participant: "a"},
		{Kind: Arrive, Barrier: "y", Participant: "k"},
	}})
	if err != nil || len(x.Completions) != 2 {
		t.Fatalf("x=%+v err=%v", x, err)
	}
	// Completions in occurrence order: x completes before y.
	if x.Completions[0].Barrier != "x" || x.Completions[1].Barrier != "y" {
		t.Fatalf("comps=%+v", x.Completions)
	}
	if !reflect.DeepEqual(x.Completions[0].Participants, []string{"a", "b"}) {
		t.Fatalf("p=%v", x.Completions[0].Participants)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	r := mk(t, Options{MaxBarriers: 1, MaxPending: 8, MaxNameBytes: 8}, Config{Name: "b", Parties: 2})
	x, _ := r.Apply(Batch{Ops: []Op{
		{Kind: Arrive, Barrier: "b", Participant: "a"},
		{Kind: Arrive, Barrier: "b", Participant: "b"},
		{Kind: Arrive, Barrier: "b", Participant: "c"},
	}})
	x.Completions[0].Participants[0] = "mut"
	s := r.Snapshot()
	if s.Barriers[0].Pending[0] != "c" {
		t.Fatalf("s=%+v", s)
	}
	s.Barriers[0].Pending[0] = "mut"
	if r.Snapshot().Barriers[0].Pending[0] != "c" {
		t.Fatal("snapshot aliases registry state")
	}
}

func TestRegistryGenerationSemantics(t *testing.T) {
	r := mk(t, Options{MaxBarriers: 1, MaxPending: 8, MaxNameBytes: 8}, Config{Name: "b", Parties: 2})
	if _, err := r.Apply(Batch{}); err != nil {
		t.Fatal(err)
	}
	if g := r.Snapshot().Generation; g != 0 {
		t.Fatalf("empty batch bumped generation: %d", g)
	}
	x, _ := r.Apply(Batch{Ops: []Op{{Kind: Arrive, Barrier: "b", Participant: "a"}}})
	if x.Generation != 1 {
		t.Fatalf("gen=%d", x.Generation)
	}
	_, _ = r.Apply(Batch{Ops: []Op{{Kind: Cancel, Barrier: "b", Participant: "ghost"}}})
	if g := r.Snapshot().Generation; g != 1 {
		t.Fatalf("failed batch bumped generation: %d", g)
	}
}

func TestStructuralValidationBeforeSemantics(t *testing.T) {
	r := mk(t, Options{MaxBarriers: 1, MaxPending: 8, MaxNameBytes: 8}, Config{Name: "b", Parties: 2})
	// Unknown barrier is semantic; bad kind later in batch must win with ErrInvalidInput.
	_, err := r.Apply(Batch{Ops: []Op{
		{Kind: Arrive, Barrier: "nope", Participant: "a"},
		{Kind: OpKind(99), Barrier: "b", Participant: "a"},
	}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	_, err = r.Apply(Batch{Ops: []Op{{Kind: Arrive, Barrier: "b", Participant: "bad!"}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	r := mk(t, Options{MaxBarriers: 2, MaxPending: 256, MaxNameBytes: 16},
		Config{Name: "a", Parties: 50}, Config{Name: "b", Parties: 50})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := fmt.Sprintf("p-%02d", i)
			_, _ = r.Apply(Batch{Ops: []Op{{Kind: Arrive, Barrier: "a", Participant: p}}})
			_, _ = r.Apply(Batch{Ops: []Op{{Kind: Arrive, Barrier: "b", Participant: p}}})
			_ = r.Snapshot()
		}()
	}
	wg.Wait()
	s := r.Snapshot()
	if s.Pending != 64 || s.Generation != 64 {
		t.Fatalf("s=%+v", s)
	}
}
