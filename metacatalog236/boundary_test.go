package metacatalog236

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
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if g := s.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestNameBoundaries(t *testing.T) {
	s := store(t)
	ok := []string{"a", "a-b_c", "012345678901"}
	for _, n := range ok {
		if err := s.ValidateBatch(Batch{Ops: []Op{{Put, n, []byte("v")}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "0123456789012", "é"}
	for _, n := range bad {
		if err := s.ValidateBatch(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	if err := s.ValidateBatch(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := s.ValidateBatch(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestValueLimitAndFinalCapacity(t *testing.T) {
	s := store(t) // MaxValueBytes 8, MaxTotalValueBytes 16, MaxRecords 3
	if err := s.ValidateBatch(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Mid-batch overflow that resolves by batch end must succeed.
	_, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")},
		{Put, "c", []byte("12345678")}, // temporarily 24 > 16
		{Delete, "c", nil},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Final overflow must fail and roll back.
	b := s.Snapshot()
	if _, err = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("12345678")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Records) != 2 {
		t.Fatalf("rollback: %+v vs %+v", got, b)
	}
}

func TestRecordCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")}}})
	b := s.Snapshot()
	_, err := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision {
		t.Fatal("clocks must roll back")
	}
}

func TestGetInvalidNameAndMissing(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("nope"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
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
		t.Fatal("clone must preserve clocks")
	}
	if _, err = c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if s.Stats().Records != 1 || c.Stats().Records != 0 {
		t.Fatal("clone not isolated")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
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
	w.Wait()
	st := s.Stats()
	if st.Records != 16 || st.Generation != 800 || st.NextRevision != 801 {
		t.Fatalf("%+v", st)
	}
}
