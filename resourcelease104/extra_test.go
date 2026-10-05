package resourcelease104

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x, _ := New(Options{MaxEntries: 16, MaxKeyBytes: 8})
	bad := []string{"", "A", "a b", "a.b", "toolongkey", "中文", "UP_1"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	for _, k := range []string{"a", "0", "-", "_", "abc-09_x"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 100}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Kind: 0, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if x.Snapshot().Now != 5 {
		t.Fatal(x.Snapshot().Now)
	}
}

func TestEvictBeforeOps(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "old", 2}}}); e != nil {
		t.Fatal(e)
	}
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "new", 9}}})
	if e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "new" || r.Generation != 2 {
		t.Fatal(s, r)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(b, got) {
		t.Fatalf("rollback failed: %+v vs %+v", b, got)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}, {Put, "b", 100}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 100}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(b, got) {
		t.Fatalf("rollback failed: %+v vs %+v", b, got)
	}
	if got := x.Snapshot(); got.Now != 1 || got.NextRevision != 3 {
		t.Fatal(got)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	r, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}, {Touch, "a", 10}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	r, e = x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}, {Put, "c", 5}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "c" || s.Now != 4 {
		t.Fatal(s)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	s.Entries[0].ExpiresAt = 0
	got := x.Snapshot()
	if got.Entries[0].Key != "a" || got.Entries[0].ExpiresAt != 9 {
		t.Fatal(got)
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
			k := fmt.Sprintf("key-%d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_ = x.Snapshot()
				_, _ = x.Expire(n)
			}
		}()
	}
	w.Wait()
	if _, e := x.Apply(Batch{Now: 1000}); e != nil {
		t.Fatal(e)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
}
