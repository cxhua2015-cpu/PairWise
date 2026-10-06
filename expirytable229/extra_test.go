package expirytable229

import (
	"errors"
	"sync"
	"testing"
)

func TestBoundaryClosedInterval(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("put with ExpiresAt<=Now: %v", e)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatalf("candidate expiry must drop ExpiresAt<=Now: %+v", s)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	before := x.Snapshot()
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("want ErrCapacity, got %v", e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("state must roll back: %+v", after)
	}
}

func TestRollbackNotFound(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}, {Delete, "ghost", 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Entries[0].ExpiresAt != 5 {
		t.Fatalf("time and revision must roll back: %+v", after)
	}
}

func TestTouchAndDeleteMissing(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "nope", 3}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "nope", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestValidation(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 3})
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Ab", 1}}},
		{Ops: []Op{{Put, "toolong", 1}}},
		{Ops: []Op{{Put, "a b", 1}}},
		{Ops: []Op{{Put, "a", -1}}},
		{Ops: []Op{{Put, "a", 0}}},
	}
	for i, b := range bad {
		if e := x.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "a_1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxEntries: 0, MaxKeyBytes: 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxEntries: 1, MaxKeyBytes: 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 0})
	if e != nil || r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
	r, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}})
	if r.Generation != 1 || x.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestExpireMonotonic(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 5}}})
	if _, e := x.Expire(2); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || x.Snapshot().Now != 5 {
		t.Fatal(e, gone)
	}
}

func TestCloneIndependence(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	c, e := x.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != x.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	_, _ = c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}})
	if x.Stats().Entries != 1 || c.Stats().Entries != 0 {
		t.Fatal("clone shares ownership with original")
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
			_, _ = x.Apply(Batch{Ops: []Op{{Put, k, 100}}})
			_ = x.ValidateBatch(Batch{Ops: []Op{{Put, k, 50}}})
			_ = x.Stats()
			_ = x.Snapshot()
			_, _ = x.Expire(0)
			_, _ = x.Clone()
		}()
	}
	w.Wait()
	if n := x.Stats().Entries; n != 32 {
		t.Fatal(n)
	}
}
