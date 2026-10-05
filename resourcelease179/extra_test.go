package resourcelease179

import (
	"errors"
	"fmt"
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
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "toolongkey", "中文"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}, {Put, "zz", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeTime(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}})
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "bad key", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "ok", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "ok", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestExpiryEvictsBeforeCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" || s.Now != 2 {
		t.Fatal(s)
	}
}

func TestRollbackOnNotFound(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Touch, "ghost", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	got := x.Snapshot()
	if got.Now != b.Now || got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatalf("not rolled back: %+v", got)
	}
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	got := x.Snapshot()
	if got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r0, e := x.Apply(Batch{})
	if e != nil || r0.Generation != 0 || r0.Revision != 0 {
		t.Fatal(e, r0)
	}
	r1, _ := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	if s := x.Snapshot(); s.NextRevision != 3 || s.Generation != 1 {
		t.Fatal(s)
	}
	r2, _ := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}})
	if r2.Generation != 2 || r2.Revision != 2 {
		t.Fatal(r2)
	}
}

func TestExpireMonotonicAndBoundary(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}})
	if _, e := x.Expire(0); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if s := x.Snapshot(); s.Now != 3 || len(s.Entries) != 1 {
		t.Fatal(s)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
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
			k := fmt.Sprintf("key-%d", i%16)
			for n := 0; n < 50; n++ {
				now := int64(n + 1)
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 100}, {Touch, k, now + 200}}})
				_, _ = x.Expire(now)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if s.Generation == 0 || s.NextRevision <= 1 {
		t.Fatal(s)
	}
}
