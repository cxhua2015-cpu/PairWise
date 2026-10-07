package expirytable319

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey", "中文"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 0, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 99, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
}

func TestNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "a", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestRollbackOnError(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 10}, {Put, "b", 1}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// b is expired at Now=5, but the batch fails at Touch of missing key:
	// purge, time and revision must all roll back.
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}, {Touch, "missing", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Entries) != len(before.Entries) {
		t.Fatalf("state changed: %+v -> %+v", before, after)
	}
	// capacity failure also rolls back
	_, e = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}, {Put, "e", 9}, {Put, "f", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != before.Now || len(got.Entries) != len(before.Entries) {
		t.Fatalf("capacity rollback failed: %+v", got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}})
	if e != nil || r.Generation != 2 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if s.Generation != 2 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestPurgeOnApply(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 10}}})
	if _, e := x.Apply(Batch{Now: 3}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 0
	if x.Snapshot().Entries[0].ExpiresAt != 9 {
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
			k := fmt.Sprintf("key-%02d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
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
}
