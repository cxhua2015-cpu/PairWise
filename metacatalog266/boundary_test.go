package metacatalog266

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

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(r, err)
	}
}

func TestNameBoundaries(t *testing.T) {
	s := store(t)
	for _, n := range []string{"", "A", "a b", "a.b", "abcdefghijklm", "é"} {
		if _, err := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a-b_c09", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte{}}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state exceeds record capacity, final state does not.
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("aa")}, {Put, "b", []byte("bb")}, {Put, "c", []byte("cc")},
		{Delete, "a", nil}, {Delete, "b", nil},
	}}); err != nil {
		t.Fatal(err)
	}
	if n := len(s.Snapshot().Records); n != 1 {
		t.Fatal(n)
	}
}

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("aa")}, {Put, "b", []byte("bb")}, {Put, "c", []byte("cc")},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := s.Snapshot()
	if after.Generation != before.Generation || after.NextRevision != before.NextRevision || len(after.Records) != 0 {
		t.Fatal("state not rolled back", after)
	}
}

func TestDeleteMissingRollsBackRevision(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Delete, "nope", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 1 || snap.Generation != 0 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestGetInvalidName(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("Bad"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("missing"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal("not sorted", snap)
	}
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	cs := c.Snapshot()
	if cs.Generation != 1 || cs.NextRevision != 2 {
		t.Fatal("clocks not preserved", cs)
	}
	r, _ := c.Apply(Batch{Ops: []Op{{Put, "b", []byte("z")}}})
	if r.Revision != 2 || s.Snapshot().NextRevision != 2 {
		t.Fatal("clone shares clock", r)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			}
		}()
	}
	wg.Wait()
	st := s.Stats()
	if st.Records != 16 || st.Generation != 16*50 || st.NextRevision != 16*50+1 {
		t.Fatal(st)
	}
	c, err := s.Clone()
	if err != nil || c.Stats() != st {
		t.Fatal(c, err)
	}
}
