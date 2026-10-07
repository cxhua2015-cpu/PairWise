package expirytable364

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustTable(t *testing.T, maxEntries, maxKey int) *Table {
	t.Helper()
	x, e := New(Options{MaxEntries: maxEntries, MaxKeyBytes: maxKey})
	if e != nil {
		t.Fatal(e)
	}
	return x
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -2}, {0, 0}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := mustTable(t, 8, 8)
	bad := []string{"", "ABC", "a b", "a.b", "waytoolong", "é", "a/b"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	good := []string{"a", "z09-_", "abcd", "0", "_", "-"}
	for _, k := range good {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 100}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKindAndNegativeExpiry(t *testing.T) {
	x := mustTable(t, 8, 8)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeTime(t *testing.T) {
	x := mustTable(t, 8, 8)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	// Backwards time AND invalid op: structural error wins.
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if got := x.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
}

func TestExpireClosedBoundary(t *testing.T) {
	x := mustTable(t, 8, 8)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
	// Nothing left to expire at the same instant.
	gone, e = x.Expire(3)
	if e != nil || len(gone) != 0 {
		t.Fatal(e, gone)
	}
}

func TestApplyEvictsBeforeOps(t *testing.T) {
	x := mustTable(t, 1, 8)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=3 (closed boundary), freeing capacity for "b".
	r, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	x := mustTable(t, 1, 8)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Now=5 would evict "a", but the batch overfills capacity and must
	// roll back evictions, time and revisions together.
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := mustTable(t, 4, 8)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "a", 10}, {Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.NextRevision != before.NextRevision || after.Generation != before.Generation ||
		after.Entries[0].ExpiresAt != 9 {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := mustTable(t, 4, 8)
	r, e := x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Generation != 0 {
		t.Fatal("empty batch changed generation")
	}
	r, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := mustTable(t, 4, 8)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "zz"
	s.Entries[0].ExpiresAt = 0
	gone, _ := x.Expire(0)
	_ = gone
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
	g, e := x.Expire(9)
	if e != nil || len(g) != 1 {
		t.Fatal(e, g)
	}
	g[0].Key = "zz"
	if len(x.Snapshot().Entries) != 0 {
		t.Fatal("expire result aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x := mustTable(t, 128, 16)
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 100}}})
				_ = x.Snapshot()
				_, _ = x.Expire(n)
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 32 {
		t.Fatal(len(s.Entries))
	}
	if s.NextRevision < 33 {
		t.Fatal(s.NextRevision)
	}
}
