package resourcecatalog131

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
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
}

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 6})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}, {Put, "c", []byte("56")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != 0 || got.NextRevision != 1 || len(got.Records) != 0 {
		t.Fatal(got)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}, {Put, "b", []byte("1234")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != 0 || len(got.Records) != 0 {
		t.Fatal(got)
	}
}

func TestDeleteSemantics(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Delete, "a", nil}}})
	if e != nil || r.Revision != 1 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("expected deleted")
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'x'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal(r)
	}
}

func TestPolicyEdges(t *testing.T) {
	if _, e := NewPolicy(0, []string{"a"}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	p, _ := NewPolicy(2, []string{"a"})
	if e := p.Authorize("a", 3); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.Authorize("ghost", 1); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.ReplaceActors([]string{""}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := p.Authorize("a", 0); e != nil {
		t.Fatal(e)
	}
}

func TestCoordinatorEngineFailureAudit(t *testing.T) {
	core, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	p, _ := NewPolicy(4, []string{"a"})
	c, _ := NewCoordinator(core, p)
	if _, e := NewCoordinator(nil, p); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := c.Apply("a", Batch{Ops: []Op{{Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	d := c.Decisions()
	if len(d) != 1 || d[0].Committed || d[0].Error == "" || d[0].Sequence != 1 {
		t.Fatal(d)
	}
}

func TestConcurrentCoordinator(t *testing.T) {
	core, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	p, _ := NewPolicy(2, []string{"alice", "bob"})
	c, _ := NewCoordinator(core, p)
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			actor := "alice"
			if i%2 == 1 {
				actor = "bob"
			}
			_, _ = c.Apply(actor, Batch{Ops: []Op{{Put, fmt.Sprintf("k%d", i%8), []byte("v")}}})
			_ = c.Decisions()
		}()
	}
	w.Wait()
	ds := c.Decisions()
	if len(ds) != 32 {
		t.Fatal(len(ds))
	}
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("non-contiguous sequence", i, d.Sequence)
		}
	}
	committed := 0
	for _, d := range ds {
		if d.Committed {
			committed++
		}
	}
	if committed != int(core.Snapshot().Generation) {
		t.Fatal(committed, core.Snapshot().Generation)
	}
}

func TestConcurrentPolicySwap(t *testing.T) {
	core, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	p, _ := NewPolicy(1, []string{"a"})
	c, _ := NewCoordinator(core, p)
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(2)
		go func() {
			defer w.Done()
			_ = p.ReplaceActors([]string{fmt.Sprintf("actor%d", i)})
		}()
		go func() {
			defer w.Done()
			_, _ = c.Apply(fmt.Sprintf("actor%d", i), Batch{Ops: []Op{{Put, "k", []byte("v")}}})
		}()
	}
	w.Wait()
	if !reflect.DeepEqual(c.Decisions(), c.Decisions()) {
		t.Fatal("decisions unstable")
	}
}
