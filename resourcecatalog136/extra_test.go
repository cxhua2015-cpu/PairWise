package resourcecatalog136

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {1, 1, 1, -1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(r, err)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Transiently exceeds record capacity but ends within limits.
	r, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}, {Put, "b", []byte("z")}, {Delete, "a", nil}}})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be deleted")
	}
	// Total value bytes exceeded at batch end.
	if _, err = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("12345")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestDeleteMissingRollsBackRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
}

func TestUnknownKindAndExtraValueLimits(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "this-name-is-too-long", nil}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentCoordinator(t *testing.T) {
	core, _ := New(Options{MaxRecords: 64, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 512})
	policy, _ := NewPolicy(2, []string{"alice", "bob"})
	coord, _ := NewCoordinator(core, policy)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := "alice"
			if i%2 == 1 {
				actor = "bob"
			}
			if i%7 == 0 {
				_ = policy.ReplaceActors([]string{"alice", "bob"})
			}
			_, _ = coord.Apply(actor, Batch{Ops: []Op{{Put, fmt.Sprintf("k-%d", i), []byte("v")}}})
			_ = coord.Decisions()
			_ = core.Snapshot()
		}()
	}
	wg.Wait()
	ds := coord.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("sequence gap at %d: %d", i, d.Sequence)
		}
	}
}

func TestPolicyRejectsInvalidConfig(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	p, _ := NewPolicy(1, []string{"a"})
	if err := p.ReplaceActors([]string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := p.Authorize("ghost", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 2); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}
