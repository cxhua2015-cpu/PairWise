package expirytable224

import (
	"errors"
	"sync"
	"testing"
)

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestEmptyBatchNoOp(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 || x.Snapshot().Now != 0 {
		t.Fatal(e, r, x.Snapshot())
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	// "a" would be evicted on the candidate (ExpiresAt 2 <= Now 4); the failing
	// Touch must roll back the eviction, time and revision.
	_, e := x.Apply(Batch{Now: 4, Ops: []Op{{Touch, "c", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	got := x.Snapshot()
	if got.Now != b.Now || got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Entries) != len(b.Entries) {
		t.Fatalf("no rollback: %+v vs %+v", got, b)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 5}}}); e != nil {
		t.Fatal(e)
	}
	_, e := x.Apply(Batch{Ops: []Op{{Put, "b", 6}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "a" || s.Generation != 1 {
		t.Fatalf("no rollback: %+v", s)
	}
}

func TestKeyCharset(t *testing.T) {
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey", "é"} {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 3}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	for _, k := range []string{"a", "z-0_", "12345678"} {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 3}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestExpireTimeMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				_ = x.ValidateBatch(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				if n%5 == 0 {
					_, _ = x.Expire(n)
				}
				if n%7 == 0 {
					c, err := x.Clone()
					if err == nil {
						_ = c.Snapshot()
					}
				}
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries != len(x.Snapshot().Entries) {
		t.Fatal("stats/snapshot mismatch")
	}
}

func TestCloneIndependence(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 50}}})
	c, e := x.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if _, e := c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}, {Put, "b", 60}}}); e != nil {
		t.Fatal(e)
	}
	if len(x.Snapshot().Entries) != 1 || x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("clone mutation leaked")
	}
	if c.Snapshot().Generation != x.Snapshot().Generation+1 {
		t.Fatal("generation not preserved/independent")
	}
}
