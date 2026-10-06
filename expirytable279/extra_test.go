package expirytable279

import (
	"errors"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	x := table(t)
	bad := []Batch{
		{Now: -1, Ops: []Op{{Put, "a", 5}}},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 5}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 5}}},
		{Ops: []Op{{Put, "", 5}}},
		{Ops: []Op{{Put, "Upper", 5}}},
		{Ops: []Op{{Put, "bad key", 5}}},
		{Ops: []Op{{Put, "toolongkey", 5}}},
		{Ops: []Op{{Put, "a", 0}}}, // ExpiresAt <= Now
		{Ops: []Op{{Touch, "a", 0}}},
		{Ops: []Op{{Delete, "bad?", 0}}},
	}
	for i, b := range bad {
		if err := x.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := x.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Put, "ok_key-1", 5}}}); err != nil {
		t.Fatal(err)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Ops: []Op{{Touch, "missing", 5}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Failed batch rolls back expiry, time and revision.
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}}})
	before := x.Snapshot()
	_, err := x.Apply(Batch{Now: 10, Ops: []Op{{Put, "b", 20}, {Delete, "ghost", 0}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("rollback: %+v", after)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 5}}})
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "b", 6}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "a" || s.Generation != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestGenerationCounts(t *testing.T) {
	x := table(t)
	r, err := x.Apply(Batch{Now: 1}) // empty batch: no generation bump
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	r, _ = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 10}, {Touch, "a", 11}}})
	if r.Generation != 2 || r.Revision != 3 {
		t.Fatal(r)
	}
}

func TestExpireMonotonicAndClosed(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	gone, err := x.Expire(5) // closed interval: ExpiresAt <= now
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(gone, err)
	}
	if _, err := x.Expire(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if z := x.Stats(); z.Entries != 1 || z.Now != 5 {
		t.Fatalf("%+v", z)
	}
}

func TestCloneIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if z := c.Stats(); z.Entries != 1 || z.Now != 1 || z.Generation != 1 || z.NextRevision != 2 {
		t.Fatalf("%+v", z)
	}
	_, _ = c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}})
	if x.Stats().Entries != 1 || c.Stats().Entries != 0 {
		t.Fatal("clone aliases original")
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
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			for j := 1; j <= 20; j++ {
				now := int64(j)
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				_ = x.ValidateBatch(Batch{Now: now, Ops: []Op{{Put, k, now + 100}}})
				if _, err := x.Clone(); err != nil {
					t.Error(err)
				}
				_, _ = x.Expire(now)
			}
		}()
	}
	w.Wait()
	if z := x.Stats(); z.Entries > 128 {
		t.Fatal(z)
	}
}
