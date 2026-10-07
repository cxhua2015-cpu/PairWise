package expirytable429

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestOptionsAndKeyValidation(t *testing.T) {
	if _, err := New(Options{MaxEntries: 0, MaxKeyBytes: 4}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := New(Options{MaxEntries: 4, MaxKeyBytes: -1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	x := table(t)
	for _, key := range []string{"", "Bad", "a b", "a.b", "toolongkey", "é"} {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, key, 5}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: %v", key, err)
		}
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Put, "ok_key-1", 5}}}); err != nil {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 5}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Now: 5, Ops: []Op{{Put, "a", 5}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestClosedBoundaryExpiry(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}}}); err != nil {
		t.Fatal(err)
	}
	// Applying at Now=2 evicts "a" (ExpiresAt <= Now) before ops run.
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}}}); err != nil {
		t.Fatal(err)
	}
	snap := x.Snapshot()
	if len(snap.Entries) != 2 || snap.Entries[0].Key != "b" || snap.Entries[1].Key != "c" {
		t.Fatalf("snapshot: %+v", snap)
	}
	gone, err := x.Expire(9)
	if err != nil || len(gone) != 2 {
		t.Fatalf("expire: %v %v", gone, err)
	}
	if _, err := x.Expire(8); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestRollbackOnCapacityAndNotFound(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}, {Put, "b", 100}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// Eviction of expired entries must roll back with the failed batch.
	if _, err := x.Apply(Batch{Now: 200, Ops: []Op{{Put, "c", 300}, {Put, "d", 300}, {Put, "e", 300}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("capacity rollback: before=%+v got=%+v", before, got)
	}
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "missing", 50}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Kind: Delete, Key: "missing"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("notfound rollback: before=%+v got=%+v", before, got)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, err := x.Apply(Batch{Now: 3})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	if x.Snapshot().Now != 3 {
		t.Fatal("empty batch should still advance time")
	}
	r, err = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 9}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatalf("non-empty batch: %+v %v", r, err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _, _, _ = x.Preview(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_ = x.Stats()
				_ = x.Snapshot()
				if n%5 == 0 {
					_, _ = x.Expire(n)
					_, _ = x.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries != 32 || s.Now != 20 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestConcurrentPreviewIsolation(t *testing.T) {
	x, _ := New(Options{MaxEntries: 8, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 50}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for n := 0; n < 50; n++ {
				r, snap, st, err := x.Preview(Batch{Now: 1, Ops: []Op{{Touch, "a", 60}}})
				if err != nil {
					return
				}
				if snap.Entries[0].Revision != r.Revision || st.NextRevision != snap.NextRevision {
					t.Error("inconsistent preview")
					return
				}
			}
		}()
	}
	w.Wait()
	if got := x.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatal("preview mutated receiver under concurrency")
	}
}
