package metacatalog296

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
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	for _, bad := range []string{"", "AB", "a b", "a/b", "abcde", "é", "a.b"} {
		if err := s.ValidateBatch(Batch{Ops: []Op{{Put, bad, nil}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", bad, err)
		}
	}
	for _, good := range []string{"a", "z-0_", "1234"} {
		if err := s.ValidateBatch(Batch{Ops: []Op{{Put, good, nil}}}); err != nil {
			t.Fatalf("name %q: %v", good, err)
		}
	}
}

func TestStructuralBeforeState(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	// Delete of a missing name would fail with ErrNotFound, but the later
	// structural error must win because validation precedes state reads.
	_, err := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("x")}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := s.ValidateBatch(Batch{Ops: []Op{{Kind: Kind(99), Name: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := s.ValidateBatch(Batch{Ops: []Op{{Put, "a", make([]byte, 5)}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}, {Put, "c", nil}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if z := s.Stats(); z.Records != 0 || z.Generation != 0 || z.NextRevision != 1 {
		t.Fatalf("rollback: %+v", z)
	}
	// Total value bytes overflow also rolls back.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("345")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if z := s.Stats(); z.Records != 0 || z.NextRevision != 1 {
		t.Fatalf("rollback: %+v", z)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	r1, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	if err != nil || r1.Generation != 1 {
		t.Fatal(r1, err)
	}
	r2, err := s.Apply(Batch{})
	if err != nil || r2.Generation != 1 || r2.Revision != 1 || len(r2.Changed) != 0 {
		t.Fatal(r2, err)
	}
}

func TestDeleteMissingAndReput(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Delete, "a", nil}, {Put, "a", []byte("w")}}})
	if err != nil || r.Revision != 2 {
		t.Fatal(r, err)
	}
	rec, ok, _ := s.Get("a")
	if !ok || string(rec.Value) != "w" || rec.Revision != 2 {
		t.Fatal(rec, ok)
	}
}

func TestGetInvalidName(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, _, err := s.Get("Bad"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("ok"); err != nil || ok {
		t.Fatal(ok, err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "a", []byte("1")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'x'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "1" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got.Generation != 1 || got.NextRevision != 2 {
		t.Fatalf("clocks: %+v", got)
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if z := s.Stats(); z.Records != 1 {
		t.Fatal("clone mutation leaked")
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
	z := s.Stats()
	if z.Records != 32 || z.Generation != 640 || z.NextRevision != 641 {
		t.Fatalf("%+v", z)
	}
	if snap := s.Snapshot(); snap.Generation != z.Generation || snap.NextRevision != z.NextRevision {
		t.Fatal("snapshot/stats diverge")
	}
}

func TestConcurrentClone(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("k%d", i)
			for j := 0; j < 10; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				clone, err := s.Clone()
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = clone.Apply(Batch{Ops: []Op{{Put, "tmp", []byte("x")}}})
			}
		}()
	}
	wg.Wait()
	if _, ok, _ := s.Get("tmp"); ok {
		t.Fatal("clone write leaked into source")
	}
}
