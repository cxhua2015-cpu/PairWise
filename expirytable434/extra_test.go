package expirytable434

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
	for _, key := range []string{"", "A", "a b", "a.b", "toolongkey"} {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, key, 5}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: %v", key, err)
		}
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 5}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Put, "a", 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("expiresAt <= now must be rejected")
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Put, "abcdefgh", 9}}}); err != nil {
		t.Fatal(err)
	}
}

func TestClosedBoundaryExpire(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}}}); err != nil {
		t.Fatal(err)
	}
	gone, err := x.Expire(2)
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(err, gone)
	}
	if len(x.Snapshot().Entries) != 1 {
		t.Fatal("b must survive")
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "missing", 5}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if got := x.Snapshot(); got.Now != before.Now || got.Generation != before.Generation ||
		got.NextRevision != before.NextRevision || len(got.Entries) != 1 {
		t.Fatal("failed batch must roll back expiry, time and revision")
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if len(s.Entries) != 0 || s.Now != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("capacity failure must roll back everything: %+v", s)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, err := x.Apply(Batch{Now: 1})
	if err != nil || r.Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
	r, err = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
	r, err = x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 10}, {Delete, "a", 0}}})
	if err != nil || r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r, err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
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
			_, _, _, _ = x.Preview(Batch{Now: int64(i), Ops: []Op{{Touch, k, 2000}}})
			_, _ = x.Clone()
			_ = x.Stats()
			_ = x.Snapshot()
			_, _ = x.Expire(int64(i))
		}()
	}
	w.Wait()
}

func TestPreviewErrorParity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	if _, _, _, err := x.Preview(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if _, _, _, err := x.Preview(Batch{Now: 2, Ops: []Op{{Touch, "zz", 9}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, _, err := x.Preview(Batch{Now: 2, Ops: []Op{{Put, "bad!", 9}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if got := x.Stats(); got.Entries != 1 || got.Now != 1 {
		t.Fatal("failed previews mutated state")
	}
}
