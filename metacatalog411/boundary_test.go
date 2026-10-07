package metacatalog411

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

func TestStructuralBoundaries(t *testing.T) {
	s := store(t) // MaxNameBytes 12, MaxValueBytes 8
	cases := []Batch{
		{Ops: []Op{{Put, "", []byte("x")}}},
		{Ops: []Op{{Put, "UPPER", []byte("x")}}},
		{Ops: []Op{{Put, "dot.name", []byte("x")}}},
		{Ops: []Op{{Put, "this-name-is-too-long", []byte("x")}}},
		{Ops: []Op{{Put, "ok-name", make([]byte, 9)}}},
		{Ops: []Op{{Kind(0), "a", nil}}},
		{Ops: []Op{{Kind(3), "a", nil}}},
		{Ops: []Op{{Delete, "a", []byte("x")}}},
	}
	for i, b := range cases {
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a-b_c9", make([]byte, 8)}}}); err != nil {
		t.Fatal(err)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")},
	}}); err != nil {
		t.Fatal(err)
	}
	// Total exceeds mid-batch but a delete brings it back under the limit.
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("12345678")}, {Delete, "a", nil},
	}}); err != nil {
		t.Fatal(err)
	}
	// Over MaxRecords at batch end fails and rolls back.
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "d", []byte("1")}, {Put, "e", []byte("1")},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(s.Snapshot().Records); n != 2 {
		t.Fatal(n)
	}
	// Over total value bytes at batch end fails and rolls back.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("12345678")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if g := s.Snapshot().Generation; g != 2 {
		t.Fatal(g)
	}
}

func TestDeleteMissingRollsBackRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := s.Snapshot()
	if before.Generation != after.Generation || before.NextRevision != after.NextRevision {
		t.Fatal("clocks advanced on failed batch")
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
	names := []string{"b", "c"}
	for _, n := range names {
		_, _ = s.Apply(Batch{Ops: []Op{{Put, n, []byte("z")}}})
	}
	got := s.Snapshot().Records
	for i, want := range []string{"a", "b", "c"} {
		if got[i].Name != want {
			t.Fatal("snapshot not sorted by name")
		}
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
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, []byte("v")}}})
			}
		}()
	}
	wg.Wait()
	st := s.Stats()
	if st.Records != 32 || st.Generation != 32*20 || st.NextRevision != 32*20+1 {
		t.Fatalf("%+v", st)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if cs := c.Stats(); cs != st {
		t.Fatalf("clone stats %+v != %+v", cs, st)
	}
}
