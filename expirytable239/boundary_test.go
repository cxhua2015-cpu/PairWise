package expirytable239

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a+b"}
	for _, k := range bad {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, err)
		}
	}
	good := []string{"a", "z-0_9", "abcdefgh"}
	for _, k := range good {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}}); err != nil {
			t.Fatalf("key %q: %v", k, err)
		}
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unknown kind accepted")
	}
	if err := x.ValidateBatch(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("negative now accepted")
	}
	if err := x.ValidateBatch(Batch{Now: 5, Ops: []Op{{Put, "a", 5}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("expiresAt <= now accepted")
	}
}

func TestValidateBatchSharesSemantics(t *testing.T) {
	x := table(t)
	b := Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Touch, "missing", 3}}}
	if err := x.ValidateBatch(b); err != nil {
		t.Fatal(err)
	}
	// Structurally valid but touches a missing key: Apply fails and
	// rolls back everything, including the purge and the Put.
	if _, err := x.Apply(b); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if s.Now != 0 || s.Generation != 0 || len(s.Entries) != 0 {
		t.Fatalf("state leaked: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// Purge would not free "a" (ExpiresAt 10 > 2); adding "b" overflows.
	_, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if before.Now != after.Now || before.NextRevision != after.NextRevision ||
		len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("no rollback: %+v -> %+v", before, after)
	}
}

func TestPurgeOnApply(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "a", 2}, {Put, "b", 5}}}); err != nil {
		t.Fatal(err)
	}
	// Now=2 purges "a" (closed boundary), freeing capacity for "c".
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}}}); err != nil {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatalf("%+v", s)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, err := x.Apply(Batch{Now: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
	r, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Delete, "nope", 0}}})
	if r.Generation != 0 {
		t.Fatal("failed batch bumped generation")
	}
}

func TestMonotonicTime(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 4}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(5); err != nil {
		t.Fatal("equal now must be accepted")
	}
}

func TestCloneIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got != x.Stats() {
		t.Fatal("clone diverges at birth")
	}
	_, _ = c.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if x.Stats().Entries != 1 || c.Stats().Entries != 2 {
		t.Fatal("clone aliases original")
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
			k := fmt.Sprintf("k%02d", i)
			_, _ = x.Apply(Batch{Ops: []Op{{Put, k, 50}}})
			_, _ = x.Apply(Batch{Ops: []Op{{Touch, k, 60}}})
			_ = x.ValidateBatch(Batch{Ops: []Op{{Delete, k, 0}}})
			_ = x.Stats()
			_ = x.Snapshot()
			if c, err := x.Clone(); err == nil {
				_, _ = c.Expire(100)
			}
		}()
	}
	w.Wait()
	st := x.Stats()
	if st.Entries != 32 || st.NextRevision != 65 || st.Generation != 64 {
		t.Fatalf("%+v", st)
	}
	gone, err := x.Expire(60)
	if err != nil || len(gone) != 32 {
		t.Fatal(err, len(gone))
	}
	for i, e := range gone {
		if want := fmt.Sprintf("k%02d", i); e.Key != want || e.ExpiresAt != 60 {
			t.Fatalf("entry %d: %+v", i, e)
		}
	}
}
