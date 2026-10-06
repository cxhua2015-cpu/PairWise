package expirytable244

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

func TestKeyCharsetAndLength(t *testing.T) {
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey", "é"} {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Delete, "bad key", 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExpiryBoundaryClosed(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}}}); e != nil {
		t.Fatal(e)
	}
	// Candidate eviction removes ExpiresAt <= Now before ops run.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatalf("snapshot: %+v", s)
	}
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "b" {
		t.Fatalf("expire: %v %+v", e, gone)
	}
}

func TestRollbackCapacityAndNotFound(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Touch of a missing key fails after candidate eviction of "a"; all rolled back.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "zz", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != before.Now || got.Generation != before.Generation ||
		got.NextRevision != before.NextRevision || len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatalf("not rolled back: %+v", got)
	}
	// Final capacity failure rolls back too.
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Generation != before.Generation || len(got.Entries) != 1 {
		t.Fatalf("capacity not rolled back: %+v", got)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 0})
	if e != nil || r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatalf("empty batch: %v %+v", e, r)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	if g := x.Snapshot().Generation; g != 1 {
		t.Fatalf("generation=%d", g)
	}
}

func TestNegativeAndBackwardsTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 5}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 5}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(2); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 6}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
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
			k := fmt.Sprintf("k%02d", i)
			for n := 0; n < 5; n++ {
				if _, err := x.Apply(Batch{Ops: []Op{{Put, k, 100}}}); err != nil {
					t.Error(err)
					return
				}
				_ = x.Stats()
				_ = x.Snapshot()
				_ = x.ValidateBatch(Batch{Ops: []Op{{Touch, k, 200}}})
			}
			_, _ = x.Expire(0)
			c, err := x.Clone()
			if err == nil {
				_ = c.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 32 || s.Generation != 160 {
		t.Fatalf("entries=%d generation=%d", len(s.Entries), s.Generation)
	}
	st := x.Stats()
	if st.Entries != 32 || st.NextRevision != 161 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestCloneIndependence(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Now != 1 || c.Stats().NextRevision != 2 {
		t.Fatal("clone lost logical clocks")
	}
	if _, e := c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}}); e != nil {
		t.Fatal(e)
	}
	if len(x.Snapshot().Entries) != 1 || len(c.Snapshot().Entries) != 0 {
		t.Fatal("clone shares ownership")
	}
}
