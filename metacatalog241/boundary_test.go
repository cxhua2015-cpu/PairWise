package metacatalog241

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, opts := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, err := New(opts); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", opts, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	s := store(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Put, "", nil}}},
		{Ops: []Op{{Put, "Bad", nil}}},
		{Ops: []Op{{Put, "a b", nil}}},
		{Ops: []Op{{Put, "a.b", nil}}},
		{Ops: []Op{{Put, "toolongname123", nil}}},
		{Ops: []Op{{Put, "a", make([]byte, 9)}}},
		{Ops: []Op{{Delete, "a", []byte("x")}}},
		{Ops: []Op{{Delete, "a", []byte{}}}},
	}
	for i, b := range cases {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d validate: %v", i, err)
		}
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d apply: %v", i, err)
		}
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatalf("state mutated: %+v", snap)
	}
}

func TestValidNamesAndEmptyValue(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a-z_09", nil}}}); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := s.Get("a-z_09")
	if !ok || err != nil || rec.Value != nil {
		t.Fatal(rec, ok, err)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("22")}}}); err != nil {
		t.Fatal(err)
	}
	// Total value bytes exceed the limit mid-batch but shrink back by the end.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("3333")}, {Put, "b", nil}}}); err != nil {
		t.Fatal(err)
	}
	// Final state exceeds the record limit: full rollback.
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("4")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if after := s.Snapshot(); after.Generation != before.Generation || after.NextRevision != before.NextRevision {
		t.Fatal("clocks advanced on failure")
	}
	// Final total value bytes exceed the limit.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("4444")}, {Put, "b", []byte("4")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestDeleteMissingRollback(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); err != nil {
		t.Fatal(err)
	}
	before := s.Stats()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "missing", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if after := s.Stats(); after != before {
		t.Fatalf("stats changed: %+v -> %+v", before, after)
	}
}

func TestGetInvalidNameAndMissing(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("Bad!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("nope"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'z'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "2" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneIndependence(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got != s.Stats() {
		t.Fatal("clone diverges")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Put, "a", []byte("zz")}}}); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "1" || s.Stats().Generation != 1 {
		t.Fatal("clone write leaked into original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
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
				if err := s.ValidateBatch(Batch{Ops: []Op{{Delete, name, nil}}}); err != nil {
					t.Error(err)
				}
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, name, nil}}})
		}()
	}
	wg.Wait()
	if st := s.Stats(); st.Records != 0 || st.TotalValueBytes != 0 {
		t.Fatalf("final stats: %+v", st)
	}
}

func TestConcurrentCloneConsistency(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("c%d", i)
			for j := 0; j < 10; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				c, err := s.Clone()
				if err != nil {
					t.Error(err)
					return
				}
				if c.Stats().NextRevision != c.Snapshot().NextRevision {
					t.Error("clone clocks inconsistent")
				}
			}
		}()
	}
	wg.Wait()
}
