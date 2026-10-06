package metacatalog286

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	bad := []string{"", "abcde", "A", "a b", "a.b", "中文"}
	for _, n := range bad {
		if e := s.ValidateBatch(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", n, e)
		}
	}
	good := []string{"a", "z0-_", "0000"}
	for _, n := range good {
		if e := s.ValidateBatch(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestValueAndKindBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 64})
	if e := s.ValidateBatch(Batch{Ops: []Op{{Put, "a", []byte("xyz")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := s.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := s.ValidateBatch(Batch{Ops: []Op{{Delete, "a", []byte{}}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := s.ValidateBatch(Batch{}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("generation changed")
	}
}

func TestDeleteMissingAndRevisionContinuity(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if e != nil || r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Changed[0].Revision != 2 {
		t.Fatal(r, e)
	}
	if s.Snapshot().NextRevision != 3 {
		t.Fatal(s.Snapshot())
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state exceeds record cap, final state fits.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("y")}, {Put, "c", []byte("z")}, {Delete, "c", nil}}}); e != nil {
		t.Fatal(e)
	}
	// Final total bytes exceeded: rollback.
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("zzz")}, {Put, "b", []byte("zz")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if g := s.Snapshot(); g.Generation != b.Generation || g.NextRevision != b.NextRevision || len(g.Records) != 2 {
		t.Fatal("not rolled back", g)
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	c, e := s.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Snapshot().Generation != s.Snapshot().Generation || c.Stats() != s.Stats() {
		t.Fatal("clocks differ")
	}
	r, _ := c.Apply(Batch{Ops: []Op{{Put, "a", []byte("w")}}})
	if r.Changed[0].Revision != 2 {
		t.Fatal("revision clock not preserved")
	}
	if v, _, _ := s.Get("a"); string(v.Value) != "v" {
		t.Fatal("clone mutated original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Put, k, []byte("w")}}})
			_, _, _ = s.Get(k)
			_ = s.Snapshot()
			_ = s.Stats()
			_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, nil}}})
			if i%4 == 0 {
				_, _ = s.Clone()
			}
		}()
	}
	w.Wait()
	z := s.Stats()
	if z.Records != 32 || z.NextRevision != 65 || z.TotalValueBytes != 32 {
		t.Fatalf("%+v", z)
	}
}
