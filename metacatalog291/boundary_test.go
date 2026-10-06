package metacatalog291

import (
	"errors"
	"sync"
	"testing"
)

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 2, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	good := []string{"a", "ab", "a-", "_0", "z9"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "abc", "A", "a b", "a.b", "é"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", n, e)
		}
	}
}

func TestValueAndTotalCapacity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 2, MaxTotalValueBytes: 3})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xxx")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("ab")}, {Put, "b", []byte("cd")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if z := s.Stats(); z.Records != 0 || z.Generation != 0 || z.NextRevision != 1 {
		t.Fatalf("rollback: %+v", z)
	}
}

func TestEndOfBatchCapacity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	// Intermediate state exceeds MaxRecords but final state does not.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("y")}, {Delete, "a", nil}}}); e != nil {
		t.Fatal(e)
	}
	if len(s.Snapshot().Records) != 1 {
		t.Fatal("expected one record")
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s, _ := New(Options{1, 4, 4, 8})
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 {
		t.Fatal(x, e)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
}

func TestDeleteMissingAndUnknownKind(t *testing.T) {
	s, _ := New(Options{2, 4, 4, 8})
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}, {Kind(99), "b", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	s, _ := New(Options{4, 8, 4, 16})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	c, e := s.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Snapshot().Generation != s.Snapshot().Generation || c.Stats().NextRevision != s.Stats().NextRevision {
		t.Fatal("clocks not preserved")
	}
	r, _, _ := c.Get("a")
	r.Value[0] = 'X'
	r2, _, _ := s.Get("a")
	if string(r2.Value) != "v" {
		t.Fatal("clone aliases value")
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
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			_, _, _ = s.Get(k)
			_ = s.Stats()
			_ = s.Snapshot()
			if i%4 == 0 {
				_, _ = s.Clone()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if snap.Records == nil || len(snap.Records) != 0 {
		t.Fatal(len(snap.Records))
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}
