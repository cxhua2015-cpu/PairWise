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

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if s.Snapshot().NextRevision != 1 {
		t.Fatal(s.Snapshot())
	}
}

func TestUnknownKindAndExtraLimits(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "this-name-is-too-long", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTotalValueCapacityRollback(t *testing.T) {
	s := store(t) // MaxTotalValueBytes 16
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestDeleteWithinBatchAndRevisionContinuity(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if e != nil || x.Revision != 2 || len(x.Changed) != 1 || x.Changed[0].Name != "b" {
		t.Fatal(e, x)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be deleted")
	}
	if s.Snapshot().NextRevision != 3 {
		t.Fatal(s.Snapshot())
	}
}

func TestGetDeepCopyAndSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'q'
	r, ok, e := s.Get("a")
	if e != nil || !ok || string(r.Value) != "xy" {
		t.Fatal(e, r)
	}
}

func TestConcurrentMixedLoad(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	p, _ := NewPolicy(4, []string{"alice", "bob"})
	c, _ := NewCoordinator(s, p)
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			actor := "alice"
			if i%3 == 0 {
				actor = "mallory"
			}
			name := fmt.Sprintf("k%d", i%8)
			_, _ = c.Apply(actor, Batch{Ops: []Op{{Put, name, []byte("v")}}})
			_, _, _ = s.Get(name)
			_ = s.Snapshot()
			_ = c.Decisions()
			if i%5 == 0 {
				_ = p.ReplaceActors([]string{"alice", "bob"})
			}
		}()
	}
	w.Wait()
	ds := c.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("audit sequence gap", d)
		}
	}
}

func TestPolicyValidationAndCoordinatorNil(t *testing.T) {
	if _, e := NewPolicy(0, []string{"a"}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	p, _ := NewPolicy(2, nil)
	if e := p.ReplaceActors([]string{"bad?"}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := p.Authorize("nobody", 1); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if _, e := NewCoordinator(nil, p); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
}
