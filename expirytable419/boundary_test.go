package expirytable419

import (
	"errors"
	"sync"
	"testing"
)

func TestOptionsAndKeyValidation(t *testing.T) {
	if _, e := New(Options{0, 8}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{4, 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x, _ := New(Options{4, 8})
	for _, key := range []string{"", "ABC", "a b", "abcdefghi", "a.b", "中文"} {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, key, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", key, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "a-z_0", 1}}}); e != nil {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestClosedIntervalBoundary(t *testing.T) {
	x, _ := New(Options{4, 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}}}); e != nil {
		t.Fatal(e)
	}
	// Entry "a" (ExpiresAt=2) is evicted by a batch at Now=2 before ops run.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "a" || s.Now != 1 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestRollbackCapacityAndRevision(t *testing.T) {
	x, _ := New(Options{1, 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "b", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Generation != before.Generation || after.NextRevision != before.NextRevision ||
		after.Now != before.Now || len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("state leaked: %+v -> %+v", before, after)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	x, _ := New(Options{2, 8})
	r, e := x.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Ops: []Op{{Put, "a", 5}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{128, 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				_ = x.ValidateBatch(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
			}
		}()
	}
	w.Wait()
	z := x.Stats()
	if z.Entries != 16 || z.Now != 20 {
		t.Fatalf("%+v", z)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(Batch{Now: 21, Ops: []Op{{Delete, "a", 0}}}); err != nil {
		t.Fatal(err)
	}
	if x.Stats().Entries != 16 || c.Stats().Entries != 15 {
		t.Fatal("clone not isolated")
	}
}
