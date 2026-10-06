package metacatalog271

import (
	"errors"
	"fmt"
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

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if g := s.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state exceeds record capacity, final state does not.
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "a", nil}}})
	if e != nil || len(r.Changed) != 2 {
		t.Fatal(e, r)
	}
	if n := len(s.Snapshot().Records); n != 2 {
		t.Fatal(n)
	}
}

func TestCapacityExceededRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 3})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Records) != 0 {
		t.Fatal("no rollback", got)
	}
}

func TestRevisionContinuity(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if r1.Revision != 2 || r1.Changed[0].Revision != 1 || r1.Changed[1].Revision != 2 {
		t.Fatal(r1)
	}
	// Failed batch must not consume revisions.
	_, _ = s.Apply(Batch{Ops: []Op{{Delete, "zz", nil}}})
	r2, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if r2.Revision != 3 {
		t.Fatal(r2.Revision)
	}
}

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 2, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	for _, bad := range []string{"", "abc", "A", "a b", "a.b", "é"} {
		if e := s.ValidateBatch(Batch{Ops: []Op{{Put, bad, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", bad, e)
		}
	}
	for _, good := range []string{"a", "ab", "a-", "_0", "z9"} {
		if e := s.ValidateBatch(Batch{Ops: []Op{{Put, good, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", good, e)
		}
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	if e := s.ValidateBatch(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := s.ValidateBatch(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	c, e := s.Clone()
	if e != nil {
		t.Fatal(e)
	}
	a, b := s.Stats(), c.Stats()
	if a != b {
		t.Fatal(a, b)
	}
	// Mutating the clone must not affect the original.
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("zz")}}})
	if _, ok, _ := s.Get("a"); !ok {
		t.Fatal("clone mutation leaked")
	}
	r, _, _ := c.Get("a")
	if r.Value != nil {
		t.Fatal("expected deleted in clone")
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
				if j%10 == 0 {
					_, _ = s.Clone()
				}
			}
		}()
	}
	w.Wait()
	st := s.Stats()
	if st.Records != 16 || st.Generation != 800 || st.NextRevision != 801 {
		t.Fatalf("%+v", st)
	}
}
