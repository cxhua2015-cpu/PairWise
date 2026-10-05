package resourcecatalog141

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestValidationBoundaries(t *testing.T) {
	s := store(t)
	base := s.Snapshot()
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a", Value: nil}}},
		{Ops: []Op{{Kind: 99, Name: "a", Value: nil}}},
		{Ops: []Op{{Put, "", nil}}},
		{Ops: []Op{{Put, "Upper", nil}}},
		{Ops: []Op{{Put, "has space", nil}}},
		{Ops: []Op{{Put, "this-name-is-too-long", nil}}},
		{Ops: []Op{{Put, "ok", []byte("012345678")}}}, // 9 > MaxValueBytes 8
	}
	for _, b := range cases {
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, err)
		}
	}
	if !reflect.DeepEqual(base, s.Snapshot()) {
		t.Fatal("failed validation mutated state")
	}
	// Boundary-valid inputs succeed.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "exactly-12ch", []byte("12345678")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")},
	}}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	// Exceeds total value bytes only at batch end.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("capacity failure did not roll back")
	}
	// Exceeds record count.
	if _, err := s.Apply(Batch{Ops: []Op{
		{Delete, "a", nil}, {Put, "c", []byte("1")}, {Put, "d", []byte("1")}, {Put, "e", []byte("1")},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("record-count failure did not roll back")
	}
}

func TestEmptyBatchAndRevision(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, err)
	}
	r, err = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if snap := s.Snapshot(); snap.NextRevision != 2 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestGetValidationAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("missing"); ok || err != nil {
		t.Fatal(ok, err)
	}
	snap := s.Snapshot()
	snap.Records = append(snap.Records, Record{Name: "x"})
	if len(s.Snapshot().Records) != 0 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestPolicyBoundaries(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(1, []string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, err := NewPolicy(2, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("b", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 3); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 2); err != nil {
		t.Fatal(err)
	}
}

func TestCoordinatorEngineFailureAudited(t *testing.T) {
	core, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	policy, _ := NewPolicy(4, []string{"a"})
	coord, err := NewCoordinator(core, policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCoordinator(nil, policy); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := coord.Apply("a", Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	d := coord.Decisions()
	if len(d) != 1 || d[0].Sequence != 1 || d[0].Committed || d[0].Error == "" {
		t.Fatal(d)
	}
}

func TestConcurrentCoordinator(t *testing.T) {
	core, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	policy, _ := NewPolicy(2, []string{"alice", "bob"})
	coord, _ := NewCoordinator(core, policy)
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
			if i%5 == 0 {
				_ = policy.ReplaceActors([]string{"alice", "bob"})
			}
			_, _ = coord.Apply(actor, Batch{Ops: []Op{{Put, fmt.Sprintf("k%02d", i), []byte("v")}}})
			_ = coord.Decisions()
		}()
	}
	wg.Wait()
	decisions := coord.Decisions()
	if len(decisions) != 16 {
		t.Fatal(len(decisions))
	}
	for i, d := range decisions {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("gap in audit sequence at %d: %+v", i, d)
		}
	}
}
