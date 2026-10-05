package resourcecatalog146

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

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(r, err)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	before := s.Snapshot()
	full := Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("1")},
	}}
	if _, err := s.Apply(full); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Intermediate overflow that resolves by batch end must succeed.
	ok := Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")},
		{Put, "tmp", []byte("x")}, {Delete, "tmp", nil},
	}}
	if _, err := s.Apply(ok); err != nil {
		t.Fatal(err)
	}
}

func TestRevisionNotConsumedByDeleteOrFailure(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("x")}, {Delete, "zz", nil}}})
	r, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("2")}}})
	if err != nil || r.Revision != 2 {
		t.Fatal(r, err)
	}
}

func TestGetValidationAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("Bad!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("missing"); ok || err != nil {
		t.Fatal(ok, err)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "k", []byte("v")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("k")
	if string(r.Value) != "v" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentCoordinatedLoad(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	p, err := NewPolicy(2, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCoordinator(s, p)
	if err != nil {
		t.Fatal(err)
	}
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
			name := fmt.Sprintf("key-%d", i)
			_, _ = c.Apply(actor, Batch{Ops: []Op{{Put, name, []byte("v")}}})
			if i%5 == 0 {
				_ = p.ReplaceActors([]string{"alice", "carol"})
			}
			_, _, _ = s.Get(name)
			_ = s.Snapshot()
			_ = c.Decisions()
		}()
	}
	wg.Wait()
	ds := c.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("audit sequence gap", ds)
		}
	}
	if len(s.Snapshot().Records) == 0 {
		t.Fatal("no records committed")
	}
}

func TestConstructorValidation(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); err == nil {
		t.Fatal("expected error for non-positive maxOps")
	}
	if _, err := NewPolicy(1, nil); err == nil {
		t.Fatal("expected error for empty actors")
	}
	if _, err := NewCoordinator(nil, nil); err == nil {
		t.Fatal("expected error for nil dependencies")
	}
}
