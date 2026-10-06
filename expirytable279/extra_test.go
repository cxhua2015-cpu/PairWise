package expirytable279

import (
	"errors"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a/b"}
	for _, k := range bad {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, err)
		}
	}
	for _, k := range []string{"a", "a-b_c", "01234567", "z9_-"} {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}}); err != nil {
			t.Fatalf("key %q: %v", k, err)
		}
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	x := table(t)
	if err := x.ValidateBatch(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Now: 5, Ops: []Op{{Put, "a", 5}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}}}); err != nil {
		t.Fatal(err)
	}
	// Evicts "a" (ExpiresAt 10 <= Now 10? no, 10 <= 10 yes) then puts two -> capacity.
	_, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if s.Now != 1 || len(s.Entries) != 1 || s.Entries[0].Key != "a" || s.NextRevision != 2 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Ops: []Op{{Touch, "a", 1}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if s := x.Snapshot(); s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, err := x.Apply(Batch{Now: 3})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if x.Snapshot().Now != 3 {
		t.Fatal("empty batch must still advance time")
	}
	r, _ = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 9}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "a", 9}, {Delete, "a", 0}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
}

func TestExpireBoundaryAndTime(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	gone, err := x.Expire(5)
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(gone, err)
	}
	if _, err := x.Expire(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 4}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestCloneIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}}); err != nil {
		t.Fatal(err)
	}
	if len(x.Snapshot().Entries) != 1 || len(c.Snapshot().Entries) != 0 {
		t.Fatal("clone aliases original")
	}
	if x.Snapshot().NextRevision != c.Snapshot().NextRevision {
		t.Fatal("clone must preserve logical clocks")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			_, _ = x.Apply(Batch{Now: int64(i), Ops: []Op{{Put, k, 1000}}})
			_ = x.Stats()
			_ = x.Snapshot()
			_, _ = x.Clone()
			_, _ = x.Expire(int64(i))
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries != len(x.Snapshot().Entries) {
		t.Fatal("stats inconsistent with snapshot")
	}
}
