package devicelease

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "toolongkey", "中文"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 5}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 5}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndBatchAtomicValidation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// One bad op rejects the whole batch; nothing is applied.
	_, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Kind(9), "b", 1}}})
	if !errors.Is(e, ErrInvalidInput) || len(x.Snapshot().Entries) != 0 {
		t.Fatal(e)
	}
}

func TestMonotonicTimeAndExpiryEviction(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// Next batch at Now=2 evicts "a" (ExpiresAt <= Now) before its ops.
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if e != nil || len(x.Snapshot().Entries) != 1 {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "c", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Now != 2 || r.Generation != 2 {
		t.Fatal(s, r)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "a", 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Failed batch rolls back eviction, time, revision and generation.
	_, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 3}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}, {Touch, "ghost", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Entries) != 1 ||
		after.Entries[0].Key != "a" {
		t.Fatalf("rollback failed: %+v -> %+v", before, after)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Ops: []Op{{Put, "b", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if after := x.Snapshot(); after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Entries) != 1 {
		t.Fatal("capacity failure must roll back")
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Now != 1 {
		t.Fatal("time should still advance")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot shares internal state")
	}
	gone, e := x.Expire(9)
	if e != nil || len(gone) != 1 {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	if len(x.Snapshot().Entries) != 0 {
		t.Fatal("expire should have removed entry")
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
			k := fmt.Sprintf("k%02d", i)
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{
					{Put, k, n + 100},
					{Touch, k, n + 200},
				}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal("capacity violated")
	}
	for i := 1; i < len(s.Entries); i++ {
		if s.Entries[i-1].Key >= s.Entries[i].Key {
			t.Fatal("snapshot not sorted")
		}
	}
}
