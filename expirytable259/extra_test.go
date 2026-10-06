package expirytable259

import (
	"errors"
	"sync"
	"testing"
)

func TestOptionsAndKeyValidation(t *testing.T) {
	if _, err := New(Options{0, 8}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := New(Options{4, 0}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	x := table(t)
	for _, key := range []string{"", "Bad", "a b", "a.b", "toolongkey", "中文"} {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, key, 9}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: %v", key, err)
		}
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Put, "ok_key-1", 9}}}); err != nil {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 9}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Delete, "a", 3}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	_, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 100}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != 1 || len(after.Entries) != 1 || after.Entries[0].Key != "a" ||
		after.Generation != before.Generation || after.NextRevision != before.NextRevision {
		t.Fatalf("rollback failed: %+v -> %+v", before, after)
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 100}, {Touch, "ghost", 50}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != 1 {
		t.Fatalf("rollback failed: %+v -> %+v", before, after)
	}
}

func TestCandidateEvictionAndTouchExpired(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 10}}}); err != nil {
		t.Fatal(err)
	}
	// "a" expires at Now=3 inside the candidate; touching it must fail and roll back.
	if _, err := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if s.Now != 1 || len(s.Entries) != 2 {
		t.Fatalf("rollback failed: %+v", s)
	}
	// Expiry frees capacity within the same batch.
	x2, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x2.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := x2.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	if got := x2.Snapshot().Entries[0].Key; got != "b" {
		t.Fatal(got)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, err := x.Apply(Batch{Now: 5})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if x.Snapshot().Now != 5 {
		t.Fatal("empty batch should still advance time")
	}
	r, err = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
}

func TestExpireMonotonicAndClosed(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 4}, {Put, "b", 5}}}); err != nil {
		t.Fatal(err)
	}
	gone, err := x.Expire(4)
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(gone, err)
	}
	if _, err := x.Expire(3); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "c", 9}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestCloneIndependence(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 50}}}); err != nil {
		t.Fatal(err)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != x.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	if _, err := c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}}); err != nil {
		t.Fatal(err)
	}
	if len(x.Snapshot().Entries) != 1 || len(c.Snapshot().Entries) != 0 {
		t.Fatal("clone aliases original")
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
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				_ = x.ValidateBatch(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
			}
			_, _ = x.Expire(50)
			if c, err := x.Clone(); err == nil {
				_ = c.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries > 26 || s.Entries == 0 {
		t.Fatal(s)
	}
	if int64(s.Generation) > 20*32 {
		t.Fatal(s)
	}
}
