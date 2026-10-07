package expirytable404

import (
	"errors"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {3, 0}, {-1, 8}, {3, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyCharsetAndLength(t *testing.T) {
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey", "é"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 9}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 9}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndEmptyBatch(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	r, e := x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.Generation != 0 || s.Now != 2 {
		t.Fatalf("empty batch mutated generation: %+v", s)
	}
}

func TestAlreadyExpiredPutRejected(t *testing.T) {
	x := table(t)
	if e := x.ValidateBatch(Batch{Now: 5, Ops: []Op{{Put, "a", 5}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 4}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 3}, {Put, "c", 4}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Time, revision and entries must all roll back.
	if got := x.Snapshot(); got.Now != b.Now || got.NextRevision != b.NextRevision ||
		got.Generation != b.Generation || len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatalf("no rollback: %+v vs %+v", got, b)
	}
}

func TestRollbackNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	// "a" expires at Now=50 during the candidate pass, so Touch misses.
	if _, e := x.Apply(Batch{Now: 50, Ops: []Op{{Touch, "a", 60}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != b.Now || got.NextRevision != b.NextRevision || len(got.Entries) != 1 {
		t.Fatalf("no rollback: %+v", got)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestExpireMonotonicAndBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 0 {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(3); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e = x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if x.Snapshot().Now != 5 {
		t.Fatal("expire did not advance clock")
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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

func TestCloneIndependence(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	c, e := x.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if _, e := c.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	if len(x.Snapshot().Entries) != 1 || x.Snapshot().Now != 1 {
		t.Fatal("clone mutated original")
	}
	if c.Snapshot().Generation != x.Snapshot().Generation+1 {
		t.Fatal("clone did not preserve logical clocks")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.ValidateBatch(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_ = x.Stats()
				_ = x.Snapshot()
				if n%10 == 0 {
					_, _ = x.Expire(n)
				}
				if n%25 == 0 {
					_, _ = x.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries > 128 || s.Now != 50 {
		t.Fatalf("%+v", s)
	}
}
