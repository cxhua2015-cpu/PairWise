package expirytable434

import (
	"errors"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {4, 0}, {-1, 8}, {4, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestValidateBatchStructural(t *testing.T) {
	x := table(t)
	cases := []Batch{
		{Now: -1, Ops: []Op{{Put, "a", 5}}},
		{Ops: []Op{{Kind(0), "a", 5}}},
		{Ops: []Op{{Kind(9), "a", 5}}},
		{Ops: []Op{{Put, "", 5}}},
		{Ops: []Op{{Put, "Upper", 5}}},
		{Ops: []Op{{Put, "bad key", 5}}},
		{Ops: []Op{{Put, "waytoolongkey", 5}}},
		{Ops: []Op{{Put, "a", 0}}},
		{Now: 3, Ops: []Op{{Touch, "a", 3}}},
	}
	for i, b := range cases {
		if err := x.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := x.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Put, "ok_key-1", 5}, {Delete, "ok_key-1", 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 5}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	before := x.Snapshot()
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}, {Delete, "ghost", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision || len(after.Entries) != 0 {
		t.Fatalf("rollback leaked: %+v", after)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); err != nil {
		t.Fatal(err)
	}
	_, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if s.Now != 1 || len(s.Entries) != 1 || s.Entries[0].Key != "a" {
		t.Fatalf("capacity rollback leaked: %+v", s)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, err := x.Apply(Batch{Now: 1})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if x.Snapshot().Now != 1 {
		t.Fatal("empty batch must still advance time")
	}
	r, err = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestExpireMonotonicAndBoundary(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); err != nil {
		t.Fatal(err)
	}
	// Empty batch at Now=5 expires "a" at the closed boundary ExpiresAt <= Now.
	if _, err := x.Apply(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
	if _, err := x.Expire(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	gone, err := x.Expire(6)
	if err != nil || len(gone) != 1 || gone[0].Key != "b" {
		t.Fatal(err, gone)
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != x.Stats() {
		t.Fatal("clone diverged")
	}
	if _, err := c.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	if len(x.Snapshot().Entries) != 1 || x.Snapshot().Now != 3 {
		t.Fatal("clone mutated original")
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
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Snapshot()
				_ = x.Stats()
				_, _, _, _ = x.Preview(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_, _ = x.Expire(n)
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 || s.NextRevision < 2 {
		t.Fatalf("bad final state: %+v", s)
	}
}
