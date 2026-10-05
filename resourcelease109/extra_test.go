package resourcelease109

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x, _ := New(Options{MaxEntries: 16, MaxKeyBytes: 8})
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a/b"}
	for _, k := range bad {
		_, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	for _, k := range []string{"a", "0", "-", "_", "abcd-ef_" /* 8 bytes */, "abcdefgh"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 100}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKindAndNegativeValues(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestStructureBeforeTime(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 10}}})
	// Stale time AND invalid key: structural error wins.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Kind: 7, Key: "a", ExpiresAt: 1}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	if !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestExpiryBeforeOpsInBatch(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}}})
	// At Now=3, "a" expires (closed bound) before Touch runs.
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "a", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Delete of expired key also fails, and state rolled back.
	if len(x.Snapshot().Entries) != 1 {
		t.Fatal(x.Snapshot())
	}
	// Put of same key at the boundary succeeds because expiry ran first.
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if got := x.Snapshot().Entries[0].ExpiresAt; got != 9 {
		t.Fatal(got)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	r, _ := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}, {Put, "b", 100}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 100}, {Touch, "a", 200}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("state changed: %+v -> %+v", before, after)
	}
	if after.Generation != r.Generation || after.NextRevision != 3 || after.Now != 1 {
		t.Fatalf("counters changed: %+v", after)
	}
}

func TestRollbackOnNotFound(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}})
	before := x.Snapshot()
	// Touch allocates a revision before Delete fails; all must roll back.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 50}, {Delete, "missing", 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed")
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestGenerationAndRevisionCounters(t *testing.T) {
	x := table(t)
	r, _ := x.Apply(Batch{Ops: nil})
	if r.Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
	r, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 10}, {Put, "b", 10}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
	s := x.Snapshot()
	if s.Generation != 2 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestExpireClosedBoundAndMonotonic(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}, {Put, "c", 4}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "c" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e = x.Expire(5)
	if e != nil || len(gone) != 0 {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 10}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	s.Entries = append(s.Entries, Entry{Key: "z"})
	again := x.Snapshot()
	if len(again.Entries) != 1 || again.Entries[0].Key != "a" {
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
			k := fmt.Sprintf("k%02d", i)
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Ops: []Op{{Touch, k, n + 200}}})
				_, _ = x.Expire(0)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 32 {
		t.Fatal(len(s.Entries))
	}
	seen := map[uint64]bool{}
	for _, e := range s.Entries {
		if seen[e.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[e.Revision] = true
	}
	if s.NextRevision != 32*50*2+1 {
		t.Fatal(s.NextRevision)
	}
}
