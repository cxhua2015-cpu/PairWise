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

func TestNameAndValueBoundaries(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "", []byte("v")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "ABCDEFGHIJK", []byte("v")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "UPPER", []byte("v")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "ok-1_2", []byte("123456789")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "ok-1_2", []byte("12345678")}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("bad name"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1111")}, {Put, "b", []byte("2222")}}}); err != nil {
		t.Fatal(err)
	}
	b := s.Snapshot()
	// Net count stays 2 but transient state would exceed limits; must succeed.
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3333")}}}); err != nil {
		t.Fatal(err)
	}
	// Exceeding total bytes at batch end must fail and roll back.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("9999")}, {Put, "d", []byte("1")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != b.Generation+1 || len(got.Records) != 2 {
		t.Fatal(got)
	}
}

func TestDeleteMissingAndRevisionGapFree(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if s.Snapshot().NextRevision != 1 {
		t.Fatal("failed batch must not consume revisions")
	}
	r, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Delete, "a", nil}, {Put, "b", []byte("w")}}})
	if err != nil || r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r, err)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be deleted")
	}
}

func TestSnapshotDeepCopyAndOrder(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "a", []byte("1")}}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'x'
	r, _, _ := s.Get("a")
	if string(r.Value) != "1" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(1, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, _ := NewPolicy(2, []string{"a"})
	if err := p.ReplaceActors(nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 3); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("nobody", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 2); err != nil {
		t.Fatal(err)
	}
}

func TestCoordinatorEngineFailureAudit(t *testing.T) {
	core, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	p, _ := NewPolicy(4, []string{"a"})
	c, err := NewCoordinator(core, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCoordinator(nil, p); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := c.Apply("a", Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	ds := c.Decisions()
	if len(ds) != 1 || ds[0].Sequence != 1 || ds[0].Committed || ds[0].Error == "" {
		t.Fatal(ds)
	}
}

func TestConcurrentCoordinator(t *testing.T) {
	core, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	p, _ := NewPolicy(1, []string{"alice"})
	c, _ := NewCoordinator(core, p)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(2)
		go func() {
			defer wg.Done()
			actor := "alice"
			if i%2 == 0 {
				actor = "mallory"
			}
			_, _ = c.Apply(actor, Batch{Ops: []Op{{Put, fmt.Sprintf("k%d", i), []byte("v")}}})
		}()
		go func() {
			defer wg.Done()
			_ = c.Decisions()
			_ = p.ReplaceActors([]string{"alice", "carol"})
		}()
	}
	wg.Wait()
	ds := c.Decisions()
	if len(ds) != 32 {
		t.Fatal(len(ds))
	}
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("non-contiguous sequences", ds)
		}
	}
}

func TestDecisionsIsolation(t *testing.T) {
	core, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	p, _ := NewPolicy(1, []string{"a"})
	c, _ := NewCoordinator(core, p)
	if _, err := c.Apply("a", Batch{Ops: []Op{{Put, "x", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	d1 := c.Decisions()
	d2 := c.Decisions()
	if !reflect.DeepEqual(d1, d2) || &d1[0] == &d2[0] {
		t.Fatal("decisions must be independent copies")
	}
}
