package metacatalog256

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	cases := []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, -1, 1, 1}, {1, 1, -1, 1}, {1, 1, 1, -1},
	}
	for i, o := range cases {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("case %d: want ErrInvalidOptions, got %v", i, err)
		}
	}
	if _, err := New(valid); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatalf("empty batch after put: %+v %v", r, err)
	}
}

func TestStructuralValidationBoundaries(t *testing.T) {
	s := store(t) // MaxNameBytes=12, MaxValueBytes=8
	bad := []Op{
		{Put, "", []byte("v")},
		{Put, "Upper", []byte("v")},
		{Put, "has space", []byte("v")},
		{Put, "a/b", []byte("v")},
		{Put, "a.b", []byte("v")},
		{Put, "name-that-is-too-long", []byte("v")},
		{Put, "ok", []byte("012345678")}, // 9 > MaxValueBytes
		{Delete, "ok", []byte{}},         // delete must carry nil value
		{Kind(0), "ok", nil},
		{Kind(3), "ok", nil},
	}
	for i, op := range bad {
		if err := s.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d (%+v): want ErrInvalidInput, got %v", i, op, err)
		}
	}
	good := []Op{
		{Put, "a-z_0-9", []byte("")},
		{Put, "ok", make([]byte, 8)},
		{Delete, "ok", nil},
	}
	for i, op := range good {
		if err := s.ValidateBatch(Batch{Ops: []Op{op}}); err != nil {
			t.Fatalf("case %d (%+v): unexpected %v", i, op, err)
		}
	}
}

func TestValidateBatchHasNoSideEffects(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	// Structurally valid but would fail against state (missing record).
	if err := s.ValidateBatch(Batch{Ops: []Op{{Delete, "missing", nil}}}); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != before.Generation ||
		got.NextRevision != before.NextRevision || len(got.Records) != len(before.Records) {
		t.Fatal("ValidateBatch mutated state")
	}
}

func TestRevisionRollbackOnFailure(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 1 || snap.Generation != 0 || len(snap.Records) != 0 {
		t.Fatalf("failed batch leaked state: %+v", snap)
	}
	r, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	if err != nil || r.Revision != 1 {
		t.Fatalf("revision not reused after rollback: %+v %v", r, err)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, err := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	// Intermediate state exceeds both limits, final state fits.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
		{Delete, "c", nil},
		{Put, "b", []byte("zz")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Changed[0].Revision != 4 {
		t.Fatalf("changed: %+v", r.Changed)
	}
	// Final state exceeds total value bytes: whole batch rolls back.
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("444")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if after := s.Snapshot(); after.Generation != before.Generation || after.NextRevision != before.NextRevision {
		t.Fatal("capacity failure leaked clocks")
	}
}

func TestDeleteNotFoundAndGetErrors(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("missing"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("3")}, {Put, "a", []byte("1")}, {Put, "b", []byte("2")},
	}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatalf("snapshot not sorted: %+v", snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "1" {
		t.Fatal("snapshot aliases store memory")
	}
}

func TestClonePreservesClocksAndIsolates(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got.Generation != 1 || got.NextRevision != 2 || got.Records != 1 || got.TotalValueBytes != 2 {
		t.Fatalf("clone clocks: %+v", got)
	}
	// Mutate clone; original must be unaffected, and vice versa.
	if _, err := c.Apply(Batch{Ops: []Op{{Put, "a", []byte("zz")}, {Put, "b", []byte("q")}}}); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "xy" || s.Stats().Records != 1 {
		t.Fatal("clone mutation leaked into original")
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Get("a"); !ok {
		t.Fatal("original mutation leaked into clone")
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	s, err := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
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
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, []byte("v")}}})
			}
		}()
	}
	// Concurrent clone while writes are in flight.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 20; j++ {
			c, err := s.Clone()
			if err != nil {
				t.Error(err)
				return
			}
			_ = c.Snapshot()
		}
	}()
	wg.Wait()
	stats := s.Stats()
	if stats.Records != 16 || stats.Generation != 16*50 || stats.NextRevision != 16*50+1 {
		t.Fatalf("stats: %+v", stats)
	}
}
