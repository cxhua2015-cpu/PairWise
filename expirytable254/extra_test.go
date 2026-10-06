package expirytable254

import (
	"errors"
	"sync"
	"testing"
)

func TestBoundaryExtra(t *testing.T) {
	if _, e := New(Options{0, 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{1, 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x := table(t)
	// Negative Now and bad kinds/keys are structural errors.
	for _, b := range []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 9, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Upper", 1}}},
		{Ops: []Op{{Put, "a b", 1}}},
		{Ops: []Op{{Put, "toolongkey", 1}}},
		{Ops: []Op{{Put, "a", 0}}}, // ExpiresAt <= Now
	} {
		if e := x.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", b, e)
		}
	}
	// Empty batch: no generation bump, time still advances.
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 || x.Snapshot().Now != 7 {
		t.Fatal(e, r)
	}
	// Time cannot move backwards.
	if _, e = x.Apply(Batch{Now: 6}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e = x.Expire(6); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e = x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Failed batch rolls back eviction, time and revision.
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "a", 9}, {Delete, "ghost", 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("rollback: %+v -> %+v", before, after)
	}
	// Capacity failure rolls back too.
	y, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = y.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	if _, e = y.Apply(Batch{Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := y.Snapshot(); len(s.Entries) != 1 || s.Entries[0].Key != "a" || s.NextRevision != 2 {
		t.Fatalf("capacity rollback: %+v", s)
	}
}

func TestExpireBoundary(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}})
	gone, e := x.Expire(3) // closed boundary: ExpiresAt <= now
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if len(x.Snapshot().Entries) != 1 {
		t.Fatal("b must survive")
	}
}

func TestConcurrentExtra(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				if c, err := x.Clone(); err == nil {
					_ = c.Stats()
				}
				_, _ = x.Expire(n)
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Now != 20 || s.Entries > 128 {
		t.Fatalf("%+v", s)
	}
}

func TestCloneIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != x.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	_, _ = c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}, {Put, "b", 9}}})
	if len(x.Snapshot().Entries) != 1 || x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("clone mutation leaked into original")
	}
	snap := x.Snapshot()
	snap.Entries[0].Key = "zz"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}
