package metacatalog431

import (
	"errors"
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

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || r.Changed != nil {
		t.Fatal(r, err)
	}
	if g := s.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestStructuralBeforeState(t *testing.T) {
	s := store(t)
	// Delete of a missing name would be ErrNotFound, but the structural
	// error later in the batch must win because validation runs first.
	_, err := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("x")}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := s.ValidateBatch(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state exceeds total bytes, but the batch ends within limits.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1234")},
		{Put, "b", []byte("12")},
		{Delete, "a", nil},
	}})
	if err != nil || len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r, err)
	}
	// Final state exceeds capacity: rollback, revision/generation unchanged.
	before := s.Snapshot()
	_, err = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("123")}}})
	if !errors.Is(err, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(err)
	}
}

func TestRevisionContiguity(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if r2.Revision != 3 || r2.Generation != r1.Generation+1 {
		t.Fatal(r1, r2)
	}
}

func TestGetInvalidAndMissing(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("BAD"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("nope"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneClocksAndIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Snapshot(), s.Snapshot()) {
		t.Fatal("clone diverges")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if s.Stats().Records != 1 || c.Stats().Records != 0 {
		t.Fatal("clone shares state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _, _ = s.Preview(Batch{Ops: []Op{{Put, k, []byte("w")}, {Delete, k, nil}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_, _ = s.Clone()
			}
		}()
	}
	wg.Wait()
	st := s.Stats()
	snap := s.Snapshot()
	if st.Records != len(snap.Records) || st.Generation != snap.Generation || st.NextRevision != snap.NextRevision {
		t.Fatal("inconsistent final state", st, snap.Generation)
	}
}

func TestPreviewErrorParity(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	batches := []Batch{
		{Ops: []Op{{Delete, "missing", nil}}},
		{Ops: []Op{{Put, "bad!", []byte("x")}}},
		{Ops: []Op{{Put, "b", []byte("12345678")}, {Put, "c", []byte("12345678")}}},
	}
	for _, b := range batches {
		_, applyErr := func() (Result, error) {
			c, _ := s.Clone()
			return c.Apply(b)
		}()
		_, _, _, prevErr := s.Preview(b)
		if !errors.Is(prevErr, applyErr) {
			t.Fatalf("preview=%v apply=%v", prevErr, applyErr)
		}
	}
}
