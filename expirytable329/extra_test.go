package expirytable329

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -2}} {
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
}

func TestUnknownKindAndNegativeValues(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestValidationBeforeTimeCheck(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}})
	// Batch has both a bad key and a backwards Now: structural error wins.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "bad!", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCandidateExpiryBeforeOps(t *testing.T) {
	x := table(t)
	// Put a@2, then at Now=2 the entry expires (closed bound) before Touch runs.
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// The failure rolls back the candidate expiry: "a" is still present.
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
	// A successful empty batch at Now=2 expires it for real.
	if _, e := x.Apply(Batch{Now: 2}); e != nil {
		t.Fatal(e)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
}

func TestRollbackOnNotFound(t *testing.T) {
	x := table(t)
	r1, _ := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}})
	before := x.Snapshot()
	// Delete missing key fails; the Put of b and the revision spend must roll back.
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Delete, "zz", 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("state changed: %+v -> %+v", before, after)
	}
	r2, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "c", 9}}})
	if e != nil {
		t.Fatal(e)
	}
	if r2.Revision != r1.Revision+1 {
		t.Fatalf("revisions %d -> %d", r1.Revision, r2.Revision)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}})
	// a would expire at Now=2, but final capacity still fails: expiry rolls back too.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != 1 || len(after.Entries) != 2 {
		t.Fatalf("not rolled back: %+v", after)
	}
	gone, e := x.Expire(2)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
}

func TestExpireClosedBoundAndMonotonic(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 4}, {Put, "b", 5}, {Put, "c", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 2 {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r0, e := x.Apply(Batch{Now: 3})
	if e != nil || r0.Generation != 0 || x.Snapshot().Now != 3 {
		t.Fatal(e, r0)
	}
	r1, _ := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}}})
	r2, _ := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if r1.Generation != 1 || r2.Generation != 2 {
		t.Fatal(r1, r2)
	}
	if x.Snapshot().NextRevision != 4 {
		t.Fatal(x.Snapshot())
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if got := x.Snapshot().Entries[0].Key; got != "a" {
		t.Fatal("snapshot shares memory with table:", got)
	}
	gone, _ := x.Expire(9)
	if len(gone) != 1 {
		t.Fatal(gone)
	}
	gone[0].Key = "mutated"
	if len(x.Snapshot().Entries) != 0 {
		t.Fatal("expire result shares memory with table")
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
			for j := int64(1); j <= 10; j++ {
				_, _ = x.Apply(Batch{Now: j, Ops: []Op{{Put, k, j + 5}}})
				_, _ = x.Apply(Batch{Ops: []Op{{Touch, k, j + 6}}})
				_ = x.Snapshot()
				_, _ = x.Expire(j)
			}
			_, _ = x.Apply(Batch{Ops: []Op{{Delete, k, 0}}})
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal("capacity violated")
	}
}
