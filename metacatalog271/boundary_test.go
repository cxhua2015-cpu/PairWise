package metacatalog271

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

func TestStructuralValidationBoundaries(t *testing.T) {
	s := store(t)
	bad := []Op{
		{Put, "", nil},                      // empty name
		{Put, "Upper", nil},                 // uppercase
		{Put, "has space", nil},             // space
		{Put, "this-name-is-too-long", nil}, // over MaxNameBytes
		{Put, "ok", make([]byte, 9)},        // over MaxValueBytes
		{Delete, "a", []byte{}},             // delete with non-nil value
		{Kind(0), "a", nil},                 // unknown kind
		{Kind(99), "a", nil},                // unknown kind
	}
	for _, op := range bad {
		if err := s.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	good := []Op{
		{Put, "a-z0-9_-", []byte("12345678")},
		{Delete, "a-z0-9_-", nil},
	}
	for _, op := range good {
		if err := s.ValidateBatch(Batch{Ops: []Op{op}}); err != nil {
			t.Fatalf("op %+v: %v", op, err)
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

func TestGenerationIncrementsOncePerBatch(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if err != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, err)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state exceeds limits (2 records, 5 bytes) but final does not.
	_, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("xx")},
		{Put, "b", []byte("yyy")},
		{Delete, "a", nil},
		{Put, "b", []byte("z")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Final state exceeds limits: rollback.
	_, err = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("qqqq")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 1 || snap.Records[0].Name != "b" || snap.NextRevision != 4 {
		t.Fatal(snap)
	}
}

func TestRevisionRollbackOnFailure(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	before := s.Snapshot()
	_, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Put, "c", []byte("z")}, {Delete, "missing", nil}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if after := s.Snapshot(); after.NextRevision != before.NextRevision || after.Generation != before.Generation {
		t.Fatal("clocks not rolled back")
	}
}

func TestGetInvalidName(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("Bad!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("missing"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, _, _ := s.Get("a")
	if string(r.Value) != "xy" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != s.Stats() {
		t.Fatal("clone clocks diverge")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Put, "a", []byte("zz")}}})
	r, _, _ := s.Get("a")
	if string(r.Value) != "xy" {
		t.Fatal("clone aliases original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, nil}}})
			}
		}()
	}
	w.Wait()
	st := s.Stats()
	if st.Records != 32 || st.Generation != 640 || st.NextRevision != 641 {
		t.Fatalf("%+v", st)
	}
}

func TestConcurrentCloneAndStats(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%d", i)
			for j := 0; j < 10; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				c, err := s.Clone()
				if err != nil {
					t.Error(err)
					return
				}
				_ = c.Stats()
			}
		}()
	}
	w.Wait()
}
