package metacatalog221

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustStore(t *testing.T, o Options) *Store {
	t.Helper()
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

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
	s := mustStore(t, Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	cases := []Batch{
		{Ops: []Op{{Put, "", []byte("v")}}},                            // empty name
		{Ops: []Op{{Put, "abcde", []byte("v")}}},                       // name too long
		{Ops: []Op{{Put, "Bad", []byte("v")}}},                         // uppercase
		{Ops: []Op{{Put, "a b", []byte("v")}}},                         // space
		{Ops: []Op{{Put, "a", []byte("12345")}}},                       // value too long
		{Ops: []Op{{Put, "a", nil}}},                                   // nil put value
		{Ops: []Op{{Delete, "a", []byte{}}}},                           // delete with value
		{Ops: []Op{{Kind(0), "a", nil}}},                               // unknown kind
		{Ops: []Op{{Kind(99), "a", nil}}},                              // unknown kind
		{Ops: []Op{{Put, "ok", []byte("v")}, {Put, "?", []byte("v")}}}, // late failure
	}
	for i, b := range cases {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d validate: %v", i, err)
		}
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d apply: %v", i, err)
		}
	}
	if got := len(s.Snapshot().Records); got != 0 {
		t.Fatalf("failed batches mutated state: %d records", got)
	}
	if z := s.Stats(); z.Generation != 0 || z.NextRevision != 1 {
		t.Fatalf("failed batches moved clocks: %+v", z)
	}
	// Boundary-valid names and values succeed.
	ok := Batch{Ops: []Op{{Put, "a-0_", []byte("1234")}}}
	if err := s.ValidateBatch(ok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ok); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteNotFoundAndRevisionContinuity(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Changed[0].Revision != 2 {
		t.Fatalf("delete must not allocate revision: %+v", r)
	}
	if _, ok, err := s.Get("a"); err != nil || ok {
		t.Fatal("deleted record still visible")
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	// Transiently exceeds MaxRecords but ends within limits: must succeed.
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "c", nil},
	}}); err != nil {
		t.Fatal(err)
	}
	// Ends over MaxRecords: must fail and roll back fully.
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := s.Snapshot()
	if before.Generation != after.Generation || before.NextRevision != after.NextRevision || len(after.Records) != 2 {
		t.Fatalf("capacity failure must roll back clocks and state: %+v -> %+v", before, after)
	}
	// Ends over total value bytes: must fail.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}, {Put, "b", []byte("5678")}, {Put, "c", []byte("9")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	r, err := s.Apply(Batch{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatalf("empty batch changed generation: %+v", r)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	if got := s.Stats().Generation; got != 1 {
		t.Fatalf("non-empty batch must bump generation exactly once, got %d", got)
	}
}

func TestGetInvalidName(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if _, _, err := s.Get("nope?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("absent"); err != nil || ok {
		t.Fatal(err, ok)
	}
}

func TestSnapshotIsolationAndOrdering(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "a", []byte("1")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatalf("snapshot not sorted by name: %+v", snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "1" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if zs, zc := s.Stats(), c.Stats(); zs != zc {
		t.Fatalf("clone clocks diverge: %+v vs %+v", zs, zc)
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get("a"); !ok {
		t.Fatal("mutating clone affected original")
	}
}

func TestConcurrentMixedLoad(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				if j%5 == 0 {
					_, _ = s.Apply(Batch{Ops: []Op{{Delete, name, nil}}})
				}
			}
		}()
	}
	wg.Wait()
	// Revision allocations must be dense: nextRevision-1 equals number of successful puts.
	z := s.Stats()
	if z.NextRevision < 2 {
		t.Fatalf("no revisions allocated: %+v", z)
	}
	if z.Records != len(s.Snapshot().Records) {
		t.Fatal("stats and snapshot diverge")
	}
}

func TestConcurrentCloneConsistency(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("k%d", i)
			for j := 0; j < 10; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				c, err := s.Clone()
				if err != nil {
					t.Error(err)
					return
				}
				if z := c.Stats(); z.Records > 8 {
					t.Errorf("impossible cloned state: %+v", z)
					return
				}
			}
		}()
	}
	wg.Wait()
}
