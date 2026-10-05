package heartbeat

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustTable(t *testing.T, o Options) *Table {
	t.Helper()
	x, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}, {0, 0}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 8, MaxKeyBytes: 4})
	bad := []string{"", "A", "a b", "a.b", "toolong", "é", "a/b"}
	for _, k := range bad {
		if _, err := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, err)
		}
	}
	for _, k := range []string{"a", "abcd", "a-b_", "z9"} {
		if _, err := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); err != nil {
			t.Fatalf("key %q: %v", k, err)
		}
	}
}

func TestUnknownKindAndNegativeTime(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestMonotonicTime(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Equal Now is allowed (closed interval, monotone non-decreasing).
	if _, err := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
}

func TestExpireBoundaryClosed(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); err != nil {
		t.Fatal(err)
	}
	gone, err := x.Expire(5)
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(err, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestApplyExpiresBeforeOps(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "a", 2}}}); err != nil {
		t.Fatal(err)
	}
	// "a" expires at Now=3, freeing capacity for "b" in the same batch.
	if _, err := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s.Entries)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// Touch would allocate a revision, then capacity check fails on the extra Put.
	_, err := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}, {Put, "b", 9}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != 1 ||
		after.Entries[0] != before.Entries[0] {
		t.Fatalf("rollback failed: %+v -> %+v", before, after)
	}
}

func TestRollbackOnNotFound(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Touch, "ghost", 9}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != 1 || after.NextRevision != before.NextRevision || len(after.Entries) != 1 {
		t.Fatalf("rollback failed: %+v", after)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	r0, err := x.Apply(Batch{})
	if err != nil || r0.Generation != 0 {
		t.Fatal(r0, err)
	}
	if g := x.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
	r1, _ := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := x.Apply(Batch{Ops: []Op{{Touch, "a", 10}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	if s := x.Snapshot(); s.NextRevision != 4 || s.Generation != 2 {
		t.Fatal(s)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	s.Entries[0].ExpiresAt = 0
	again := x.Snapshot()
	if again.Entries[0].Key != "a" || again.Entries[0].ExpiresAt != 9 {
		t.Fatal("snapshot shares state with table")
	}
}

func TestSnapshotOrder(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 8, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "c", 9}, {Put, "a", 9}, {Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	s := x.Snapshot()
	for i, want := range []string{"a", "b", "c"} {
		if s.Entries[i].Key != want {
			t.Fatalf("entries not sorted: %+v", s.Entries)
		}
	}
}

func TestConcurrentMixed(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 128, MaxKeyBytes: 8})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("n%02d", i)
			for now := int64(1); now <= 50; now++ {
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 10}}})
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Touch, k, now + 20}}})
				_, _ = x.Expire(now)
				_ = x.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := x.Snapshot()
	if s.Generation == 0 || s.NextRevision <= 1 {
		t.Fatal(s)
	}
	if len(s.Entries) > 128 {
		t.Fatal(len(s.Entries))
	}
}
