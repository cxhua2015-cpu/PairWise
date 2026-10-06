package expirytable284

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
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -2}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestValidateBatchStructural(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 4})
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Upper", 1}}},
		{Ops: []Op{{Put, "bad key", 1}}},
		{Ops: []Op{{Put, "toolong", 1}}},
		{Ops: []Op{{Put, "a", 0}}},           // ExpiresAt <= Now
		{Now: 3, Ops: []Op{{Touch, "a", 3}}}, // closed boundary
		{Ops: []Op{{Delete, "a", 7}}},        // extra field on Delete
	}
	for i, b := range cases {
		if err := x.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	ok := Batch{Now: 1, Ops: []Op{{Put, "a_9", 2}, {Touch, "a_9", 3}, {Delete, "a_9", 0}}}
	if err := x.ValidateBatch(ok); err != nil {
		t.Fatal(err)
	}
}

func TestClosedIntervalEviction(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}}}); err != nil {
		t.Fatal(err)
	}
	// Now=2 evicts "a" (ExpiresAt <= Now) before the put, freeing capacity.
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}}}); err != nil {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatalf("%+v", s)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 100}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	_, err := x.Apply(Batch{Now: 6, Ops: []Op{{Put, "b", 200}, {Put, "c", 300}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("rollback broken: %+v -> %+v", before, after)
	}
}

func TestNotFoundAndEmptyBatch(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 1}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r, err := x.Apply(Batch{Now: 3})
	if err != nil || r.Generation != 0 || x.Snapshot().Now != 3 {
		t.Fatalf("empty batch: %v %+v", err, r)
	}
}

func TestExpireMonotonic(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 6}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Expire(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	gone, err := x.Expire(6)
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatalf("%v %+v", err, gone)
	}
}

func TestCloneIndependence(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 50}}}); err != nil {
		t.Fatal(err)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != x.Stats() {
		t.Fatal("clone diverges at birth")
	}
	if _, err := c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}}); err != nil {
		t.Fatal(err)
	}
	if x.Stats().Entries != 1 || c.Stats().Entries != 0 {
		t.Fatal("clone aliases original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 128, MaxKeyBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("key-%d", i)
			if _, err := x.Apply(Batch{Ops: []Op{{Put, k, 1000}}}); err != nil {
				t.Error(err)
			}
			if _, err := x.Apply(Batch{Ops: []Op{{Touch, k, 2000}}}); err != nil {
				t.Error(err)
			}
			_ = x.Stats()
			_ = x.Snapshot()
			_ = x.ValidateBatch(Batch{Ops: []Op{{Put, k, 5}}})
			if c, err := x.Clone(); err == nil {
				_, _ = c.Expire(int64(i))
				_ = c.Snapshot()
			}
		}()
	}
	wg.Wait()
	if got := len(x.Snapshot().Entries); got != 32 {
		t.Fatal(got)
	}
}
