package metacatalog261

import (
	"errors"
	"sync"
	"testing"
)

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "abc", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "abcd", "A", "a b", "a.b", "é"} {
		if _, err := s.Apply(Batch{Ops: []Op{{Put, bad, []byte("v")}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", bad, err)
		}
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "ok", []byte("toolong")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}, {Kind: 3, Name: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s, _ := New(Options{1, 4, 4, 4})
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(r, err)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}}}); err != nil {
		t.Fatal(err)
	}
	// Exceeds total bytes at batch end: full rollback.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if z := s.Stats(); z.Records != 2 || z.TotalValueBytes != 4 || z.Generation != 1 || z.NextRevision != 3 {
		t.Fatalf("rollback: %+v", z)
	}
	// Delete-then-put within one batch fits capacity at the end.
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Delete, "b", nil}, {Put, "c", []byte("1234")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestRevisionNotAllocatedOnFailure(t *testing.T) {
	s, _ := New(Options{4, 4, 4, 16})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("v")}, {Delete, "zz", nil}}})
	r, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("v")}}})
	if err != nil || r.Revision != 2 || r.Generation != 2 {
		t.Fatal(r, err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 32, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 128})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			}
		}()
	}
	w.Wait()
	z := s.Stats()
	if z.Records != 16 || z.Generation != 320 {
		t.Fatalf("%+v", z)
	}
	c, err := s.Clone()
	if err != nil || c.Stats() != z {
		t.Fatal(err, c.Stats(), z)
	}
}
