package resourcecatalog146

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	for _, mutate := range []func(*Options){
		func(o *Options) { o.MaxRecords = 0 },
		func(o *Options) { o.MaxNameBytes = -1 },
		func(o *Options) { o.MaxValueBytes = 0 },
		func(o *Options) { o.MaxTotalValueBytes = 0 },
	} {
		o := valid
		mutate(&o)
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("expected ErrInvalidOptions for %+v, got %v", o, err)
		}
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Unknown kind and oversized value must fail even if the name would also miss.
	for _, b := range []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Kind: Put, Name: "a", Value: make([]byte, 9)}}},
		{Ops: []Op{{Kind: Put, Name: ""}}},
		{Ops: []Op{{Kind: Put, Name: "UPPER"}}},
		{Ops: []Op{{Kind: Put, Name: "has space"}}},
		{Ops: []Op{{Kind: Put, Name: "way-too-long-name"}}},
		{Ops: []Op{{Kind: Delete, Name: "a", Value: []byte("x")}}},
	} {
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput for %+v, got %v", b, err)
		}
	}
	if got := s.Snapshot(); got.Generation != 0 || got.NextRevision != 1 || len(got.Records) != 0 {
		t.Fatalf("state mutated by invalid batch: %+v", got)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
}

func TestCapacityOnlyAtBatchEnd(t *testing.T) {
	s, err := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	// Mid-batch the store holds 3 records / 12 bytes, but ends within limits.
	r, err := s.Apply(Batch{Ops: []Op{
		{Kind: Put, Name: "a", Value: []byte("1111")},
		{Kind: Put, Name: "b", Value: []byte("2222")},
		{Kind: Put, Name: "c", Value: []byte("3333")},
		{Kind: Delete, Name: "a"},
		{Kind: Delete, Name: "c"},
	}})
	if err != nil || r.Revision != 3 || r.Generation != 1 {
		t.Fatal(r, err)
	}
	if got := len(s.Snapshot().Records); got != 1 {
		t.Fatal(got)
	}
	// Final state over the record limit must roll back everything.
	before := s.Snapshot()
	if _, err = s.Apply(Batch{Ops: []Op{
		{Kind: Put, Name: "c", Value: []byte("1")},
		{Kind: Put, Name: "d", Value: []byte("2")},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if after := s.Snapshot(); after.Generation != before.Generation || after.NextRevision != before.NextRevision {
		t.Fatal("capacity failure leaked state")
	}
	// Final state over total value bytes must fail too.
	s2, err := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s2.Apply(Batch{Ops: []Op{
		{Kind: Put, Name: "a", Value: []byte("1111")},
		{Kind: Put, Name: "b", Value: []byte("2222")},
		{Kind: Put, Name: "c", Value: []byte("3")},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s2.Snapshot(); got.Generation != 0 || len(got.Records) != 0 {
		t.Fatal("total-bytes failure leaked state")
	}
}

func TestRevisionNotAllocatedToFailedOrDelete(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: Put, Name: "a", Value: []byte("1")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: Delete, Name: "a"}}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 2 {
		t.Fatalf("delete allocated revision: %d", snap.NextRevision)
	}
	r, err := s.Apply(Batch{Ops: []Op{{Kind: Put, Name: "b", Value: []byte("2")}}})
	if err != nil || r.Revision != 2 || r.Changed[0].Revision != 2 {
		t.Fatal(r, err)
	}
}

func TestGetDeepCopyAndSnapshotIsolation(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: Put, Name: "a", Value: []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	r, ok, err := s.Get("a")
	if !ok || err != nil {
		t.Fatal(r, ok, err)
	}
	r.Value[0] = 'Q'
	snap := s.Snapshot()
	snap.Records[0].Value[1] = 'Q'
	r2, _, _ := s.Get("a")
	if string(r2.Value) != "xy" {
		t.Fatalf("internal state aliased: %q", r2.Value)
	}
	if _, ok, _ := s.Get("missing"); ok {
		t.Fatal("missing key reported found")
	}
	if _, _, err := s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, err := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPolicy(4, []string{"writer"})
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
			name := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = c.Apply("writer", Batch{Ops: []Op{{Kind: Put, Name: name, Value: []byte("v")}}})
				_, _ = c.Apply("intruder", Batch{Ops: []Op{{Kind: Put, Name: name, Value: []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = c.Decisions()
			}
		}()
	}
	wg.Wait()
	if got := len(s.Snapshot().Records); got != 16 {
		t.Fatal(got)
	}
	ds := c.Decisions()
	if len(ds) != 16*20*2 {
		t.Fatalf("expected every attempt audited, got %d", len(ds))
	}
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("non-contiguous audit sequence at %d: %d", i, d.Sequence)
		}
	}
}

func TestPolicyReplacementAtomicity(t *testing.T) {
	s := store(t)
	p, err := NewPolicy(1, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := NewCoordinator(s, p)
	if err := p.ReplaceActors(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply("a", Batch{Ops: []Op{{Kind: Put, Name: "k", Value: []byte("v")}}}); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.ReplaceActors([]string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := NewCoordinator(nil, p); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}
