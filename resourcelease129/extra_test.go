package resourcelease129

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {3, 0}, {-1, 8}, {3, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "toolongkey", "é"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestExpiryBeforeOps(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 2}}})
	// "a" expires at the batch's Now, so Touch must fail with ErrNotFound.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Re-put after expiry works and capacity was freed.
	if _, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r1, _ := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	before := x.Snapshot()
	// Expiry of "a" would free capacity, but the batch overflows first.
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Entries) != 1 ||
		after.Entries[0].Key != "a" {
		t.Fatalf("state mutated: %+v -> %+v", before, after)
	}
	if r1.Generation != 1 || after.Generation != 1 {
		t.Fatal("generation changed")
	}
}

func TestRollbackNotFound(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Delete, "missing", 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != 1 || after.NextRevision != before.NextRevision || len(after.Entries) != 1 {
		t.Fatalf("not rolled back: %+v", after)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, _ := x.Apply(Batch{Now: 1})
	if r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatal("empty batch changed generation")
	}
	if x.Snapshot().Now != 1 {
		t.Fatal("empty batch did not advance now")
	}
	r, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Delete, "a", 0}}})
	if r.Generation != 2 || x.Snapshot().NextRevision != 3 {
		t.Fatal(r)
	}
}

func TestExpireTimeMonotonic(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 9}}})
	if _, e := x.Expire(3); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if x.Snapshot().Now != 4 {
		t.Fatal("now moved")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 0
	gone, _ := x.Expire(9)
	if len(gone) != 1 {
		t.Fatal("snapshot aliases internal state")
	}
	gone[0].Key = "zz"
	if len(x.Snapshot().Entries) != 0 {
		t.Fatal("expire result aliases internal state")
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
			for j := int64(1); j <= 20; j++ {
				_, _ = x.Apply(Batch{Now: j, Ops: []Op{{Put, k, j + 100}}})
				_, _ = x.Apply(Batch{Now: j, Ops: []Op{{Touch, k, j + 200}}})
				_ = x.Snapshot()
				_, _ = x.Expire(j)
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal("capacity exceeded")
	}
	if s.NextRevision < 1 {
		t.Fatal("bad revision")
	}
}
