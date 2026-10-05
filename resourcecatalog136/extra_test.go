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
	if err != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if _, err = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestStructuralValidationBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	cases := []Batch{
		{Ops: []Op{{Put, "", []byte("v")}}},
		{Ops: []Op{{Put, "abcd", []byte("v")}}},
		{Ops: []Op{{Put, "A", []byte("v")}}},
		{Ops: []Op{{Put, "a b", []byte("v")}}},
		{Ops: []Op{{Put, "a", []byte("xyz")}}},
		{Ops: []Op{{Kind(0), "a", nil}}},
		{Ops: []Op{{Kind(3), "a", nil}}},
	}
	for _, b := range cases {
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%+v: %v", b, err)
		}
	}
	if got := s.Snapshot(); got.Generation != 0 || got.NextRevision != 1 || len(got.Records) != 0 {
		t.Fatal(got)
	}
	// Valid boundary names/values are accepted.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a_0", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestValidationPrecedesStateRead(t *testing.T) {
	s := store(t)
	// Delete of a missing name would be ErrNotFound, but the invalid op
	// earlier in the batch must win because validation runs first.
	_, err := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "ok", []byte("toolongvalue")}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Transiently 3 records / 6 bytes, but ends within capacity.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
		{Put, "c", []byte("3")},
	}})
	if err != nil || r.Generation != 1 || r.Revision != 4 {
		t.Fatal(r, err)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "b" || snap.Records[1].Name != "c" || snap.NextRevision != 5 {
		t.Fatal(snap)
	}
	// Ending over capacity fails and rolls back revisions too.
	before := s.Snapshot()
	_, err = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("x")}, {Put, "e", []byte("y")}}})
	if !errors.Is(err, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(err, s.Snapshot())
	}
	_, err = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("xxxx")}, {Put, "c", []byte("y")}}})
	if !errors.Is(err, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(err, s.Snapshot())
	}
}

func TestDeleteNotFoundRollsBackRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Delete, "nope", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after rollback")
	}
	// Next successful Put continues the revision sequence without gaps from the failed batch.
	r, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}}})
	if err != nil || r.Revision != 2 {
		t.Fatal(r, err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'z'
	snap.Records[0].Name = "zz"
	again := s.Snapshot()
	if again.Records[0].Name != "a" || string(again.Records[0].Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
	r, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("9")}}})
	if err != nil || len(r.Changed) != 1 {
		t.Fatal(r, err)
	}
	r.Changed[0].Value[0] = 'q'
	got, _, _ := s.Get("a")
	if string(got.Value) != "9" {
		t.Fatal("result aliases internal state")
	}
}

func TestGetValidationAndMissing(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("absent"); err != nil || ok {
		t.Fatal(ok, err)
	}
}

func TestPolicyBasics(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(1, []string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, err := NewPolicy(2, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Authorize("a", 2); err != nil {
		t.Fatal(err)
	}
	if err = p.Authorize("a", 3); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err = p.Authorize("z", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err = p.ReplaceActors(nil); err != nil {
		t.Fatal(err)
	}
	if err = p.Authorize("a", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}

func TestCoordinatorEngineFailureAudited(t *testing.T) {
	core, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	p, _ := NewPolicy(4, []string{"op"})
	c, err := NewCoordinator(core, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewCoordinator(nil, p); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err = NewCoordinator(core, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err = c.Apply("op", Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = c.Apply("op", Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	d := c.Decisions()
	if len(d) != 2 || d[0].Sequence != 1 || d[0].Committed || d[0].Error == "" ||
		d[1].Sequence != 2 || !d[1].Committed || d[1].Generation != 1 || d[1].Error != "" {
		t.Fatal(d)
	}
}

func TestConcurrentMixedLoad(t *testing.T) {
	core, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	p, _ := NewPolicy(2, []string{"w"})
	c, _ := NewCoordinator(core, p)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("k-%d", i)
			if i%3 == 0 {
				_, _ = c.Apply("w", Batch{Ops: []Op{{Put, name, []byte("v")}}})
			} else if i%3 == 1 {
				_, _ = core.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}, {Put, name, []byte("w")}}})
			} else {
				_, _, _ = core.Get(name)
				_ = core.Snapshot()
			}
			if i%5 == 0 {
				_ = p.ReplaceActors([]string{"w", "x"})
			}
			_ = c.Decisions()
		}()
	}
	wg.Wait()
	// Every coordinator attempt, success or failure, got a unique contiguous sequence.
	d := c.Decisions()
	for i, dec := range d {
		if dec.Sequence != uint64(i+1) {
			t.Fatalf("gap at %d: %+v", i, dec)
		}
	}
	snap := core.Snapshot()
	if snap.Generation == 0 || len(snap.Records) == 0 {
		t.Fatal(snap)
	}
}
