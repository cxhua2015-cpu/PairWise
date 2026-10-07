package metacatalog431

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	for _, o := range []Options{
		{}, {MaxRecords: 0, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: -1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 0, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: -2},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("New(%+v) = %v", o, err)
		}
	}
	if _, err := New(valid); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	before := s.Snapshot()
	r, err := s.Apply(Batch{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != before.Generation || s.Snapshot().Generation != before.Generation {
		t.Fatal("empty batch changed generation")
	}
}

func TestStructuralValidationBoundaries(t *testing.T) {
	s := store(t) // MaxNameBytes 12, MaxValueBytes 8
	cases := []struct {
		op  Op
		err error
	}{
		{Op{Put, "", []byte("v")}, ErrInvalidInput},
		{Op{Put, "UPPER", []byte("v")}, ErrInvalidInput},
		{Op{Put, "has space", []byte("v")}, ErrInvalidInput},
		{Op{Put, "toolongname123", []byte("v")}, ErrInvalidInput},
		{Op{Put, "ok", make([]byte, 9)}, ErrInvalidInput},
		{Op{Delete, "a", []byte{}}, ErrInvalidInput},
		{Op{Kind(0), "a", nil}, ErrInvalidInput},
		{Op{Kind(99), "a", nil}, ErrInvalidInput},
		{Op{Put, "a-b_c9", []byte("ok")}, nil},
		{Op{Delete, "a", nil}, nil},
	}
	for _, c := range cases {
		if err := s.ValidateBatch(Batch{Ops: []Op{c.op}}); !errors.Is(err, c.err) {
			t.Fatalf("ValidateBatch(%+v) = %v, want %v", c.op, err, c.err)
		}
	}
}

func TestValidationPrecedesStateRead(t *testing.T) {
	s := store(t)
	// Delete of a missing name would be ErrNotFound, but the structural
	// error later in the batch must win because validation runs first.
	_, err := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("v")}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestEndOfBatchCapacity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Intermediate state exceeds record capacity, final state does not.
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("v")}, {Put, "b", []byte("v")}, {Put, "c", []byte("v")}, {Delete, "a", nil},
	}}); err != nil {
		t.Fatal(err)
	}
	// Final total value bytes exceeded: whole batch rolls back.
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1234")}, {Put, "c", []byte("5")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("capacity failure did not roll back")
	}
}

func TestRevisionContinuityAndDeleteNoAlloc(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if r2.Revision != 2 || s.Snapshot().NextRevision != 3 {
		t.Fatal("delete allocated a revision")
	}
	r3, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if r3.Revision != 3 {
		t.Fatal(r3)
	}
}

func TestGetAndSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	r, ok, err := s.Get("a")
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	r.Value[0] = 'Q'
	snap := s.Snapshot()
	snap.Records[0].Value[1] = 'Q'
	r2, _, _ := s.Get("a")
	if string(r2.Value) != "xy" {
		t.Fatal("returned slices alias internal state")
	}
	if _, _, err := s.Get("bad!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get("missing"); ok {
		t.Fatal("missing key reported found")
	}
}

func TestClonePreservesClocks(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Snapshot(), c.Snapshot()) || s.Stats() != c.Stats() {
		t.Fatal("clone diverged")
	}
	r, _ := c.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if r.Revision != 2 || r.Generation != 2 {
		t.Fatalf("clone lost logical clocks: %+v", r)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("key-%02d", i)
			_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
			_, _, _, _ = s.Preview(Batch{Ops: []Op{{Put, name, []byte("w")}}})
			_, _, _ = s.Get(name)
			_ = s.Snapshot()
			_ = s.Stats()
			_, _ = s.Clone()
			_ = s.ValidateBatch(Batch{Ops: []Op{{Delete, name, nil}}})
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, name, nil}}})
		}()
	}
	wg.Wait()
	if got := len(s.Snapshot().Records); got != 0 {
		t.Fatalf("records left: %d", got)
	}
}
