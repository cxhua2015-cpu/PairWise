package metacatalog286

import (
	"errors"
	"fmt"
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

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	good := []string{"a", "abc", "a-1", "_z9"}
	for _, n := range good {
		if err := s.ValidateBatch(Batch{Ops: []Op{{Put, n, nil}}}); err != nil {
			t.Fatalf("name %q should be valid: %v", n, err)
		}
	}
	bad := []string{"", "abcd", "A", "a b", "a.b", "é", "a/b"}
	for _, n := range bad {
		if err := s.ValidateBatch(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q should be invalid: %v", n, err)
		}
	}
}

func TestStructuralBeforeState(t *testing.T) {
	s := store(t)
	// Delete of a missing name with an invalid kind later in the batch must
	// fail with ErrInvalidInput (structural) rather than ErrNotFound (state).
	_, err := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Kind(99), "x", nil}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestValueAndTotalCapacity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 3})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("toolong")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("22")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if z := s.Stats(); z.Records != 0 || z.Generation != 0 || z.NextRevision != 1 {
		t.Fatalf("rollback: %+v", z)
	}
	// Intermediate overflow is fine; only the final state is checked.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Records) != 1 {
		t.Fatal("state changed after capacity failure")
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if s.Stats().Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
}

func TestDeleteSemantics(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}}); err != nil {
		t.Fatal(err)
	}
	r, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if err != nil || r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Name != "a" || r.Changed[0].Revision != 0 {
		t.Fatal(r, err)
	}
	if _, ok, err := s.Get("a"); err != nil || ok {
		t.Fatal("deleted record still visible")
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestGetInvalidName(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("nope"); err != nil || ok {
		t.Fatal(ok, err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, _, _ := s.Get("a")
	if string(r.Value) != "xy" {
		t.Fatal("snapshot aliases store")
	}
	if snap.Records[0].Name != "a" {
		t.Fatal("snapshot not sorted by name")
	}
}

func TestCloneIndependence(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got != s.Stats() {
		t.Fatal("clone lost logical clocks")
	}
	r, _, _ := c.Get("a")
	r.Value[0] = 'Q'
	r2, _, _ := s.Get("a")
	if string(r2.Value) != "xy" {
		t.Fatal("clone aliases original values")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("k%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Delete, name, nil}}})
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, name, nil}}})
		}()
	}
	wg.Wait()
	if z := s.Stats(); z.Records != 0 || z.TotalValueBytes != 0 {
		t.Fatalf("leaked state: %+v", z)
	}
}
