package metacatalog246

import (
	"errors"
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

func TestNameAndValueBoundaries(t *testing.T) {
	s := store(t) // MaxNameBytes 12, MaxValueBytes 8
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "abcdefghijkl", []byte("12345678")}}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Op{
		{Put, "", nil},
		{Put, "abcdefghijklm", nil},
		{Put, "Upper", nil},
		{Put, "a b", nil},
		{Put, "ok", []byte("123456789")},
		{Kind(0), "ok", nil},
		{Kind(99), "ok", nil},
		{Delete, "ok", []byte{}},
	} {
		if _, err := s.Apply(Batch{Ops: []Op{bad}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", bad, err)
		}
	}
}

func TestTotalValueCapacityAtBatchEnd(t *testing.T) {
	s := store(t) // MaxTotalValueBytes 16
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}}}); err != nil {
		t.Fatal(err)
	}
	// Overwrite with smaller values: peak exceeds but final fits.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("y")}}}); err != nil {
		t.Fatal(err)
	}
	// Final total exceeds capacity: rollback.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "c", []byte("12345678")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	z := s.Stats()
	if z.Records != 2 || z.TotalValueBytes != 2 {
		t.Fatalf("rollback failed: %+v", z)
	}
}

func TestRecordCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("x")}, {Put, "c", []byte("x")}}})
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("x")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Records) != 3 {
		t.Fatal("state changed after capacity failure")
	}
	// Delete + Put within the same batch stays within the record limit.
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "d", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteMissingAndGet(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("nope"); ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, _, err := s.Get("Bad!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestValidateBatchIsSideEffectFree(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	before := s.Snapshot()
	if err := s.ValidateBatch(Batch{Ops: []Op{{Put, "b", []byte("y")}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateBatch(Batch{Ops: []Op{{Put, "bad!", nil}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Records) != len(before.Records) {
		t.Fatal("ValidateBatch mutated state")
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	zs, zc := s.Stats(), c.Stats()
	if zs != zc {
		t.Fatalf("clocks differ: %+v vs %+v", zs, zc)
	}
	r, _, _ := c.Get("a")
	r.Value[0] = 'Q'
	r2, _, _ := s.Get("a")
	if string(r2.Value) != "xy" {
		t.Fatal("clone shares value memory")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if _, ok, _ := s.Get("a"); !ok {
		t.Fatal("clone mutation affected original")
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
			k := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			}
		}()
	}
	wg.Wait()
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	z := s.Stats()
	if z.Records != len(s.Snapshot().Records) || z.Generation != c.Stats().Generation {
		t.Fatal("inconsistent state")
	}
}

func TestConcurrentRevisionUniqueness(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	const n = 32
	revs := make(chan uint64, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Apply(Batch{Ops: []Op{{Put, "k", []byte("v")}}})
			if err == nil {
				revs <- r.Revision
			}
		}()
	}
	wg.Wait()
	close(revs)
	seen := map[uint64]bool{}
	for rev := range revs {
		if seen[rev] {
			t.Fatalf("duplicate revision %d", rev)
		}
		seen[rev] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d revisions, want %d", len(seen), n)
	}
}
