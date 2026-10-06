package expirytable244

import (
	"errors"
	"sync"
	"testing"
)

func TestOptionsAndKeyValidation(t *testing.T) {
	if _, e := New(Options{MaxEntries: 0, MaxKeyBytes: 4}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxEntries: 1, MaxKeyBytes: -1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a/b", "toolongkey"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 9}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExpirySweepAndNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 5}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("put expiring at now must be rejected")
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 4}, {Put, "b", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 5}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	// "a" is swept by the candidate at Now=5 before Touch runs.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Delete, "zz", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if len(x.Snapshot().Entries) != 2 {
		t.Fatal("failed batch must roll back the sweep")
	}
}

func TestRollbackRestoresClocks(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
	before := x.Snapshot()
	_, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Touch, "zz", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision || after.Generation != before.Generation {
		t.Fatal("clocks moved on failed batch")
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "c", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 20}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.Now != 0 || len(s.Entries) != 2 {
		t.Fatal("capacity failure must roll back sweep and time")
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Now != 3 {
		t.Fatal("empty batch still advances time")
	}
	r, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}, {Touch, "a", 10}}})
	if r.Generation != 1 {
		t.Fatal("non-empty batch bumps generation exactly once")
	}
}

func TestExpireMonotonicAndIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}})
	if _, e := x.Expire(0); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(2)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mut"
	if x.Snapshot().Entries[0].Key != "b" {
		t.Fatal("returned slices must be isolated")
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mut"
	if x.Snapshot().Entries[0].Key != "b" {
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
	if c.Snapshot().NextRevision != x.Snapshot().NextRevision {
		t.Fatal("clone must preserve logical clocks")
	}
	_, _ = c.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if len(x.Snapshot().Entries) != 1 || len(c.Snapshot().Entries) != 2 {
		t.Fatal("clone shares state with original")
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
			_, _ = x.Apply(Batch{Ops: []Op{{Put, k, 1000}}})
			_ = x.Stats()
			_ = x.Snapshot()
			_, _ = x.Clone()
			_, _ = x.Expire(0)
			_ = x.ValidateBatch(Batch{Ops: []Op{{Put, k, 5}}})
		}()
	}
	w.Wait()
	if got := len(x.Snapshot().Entries); got != 32 {
		t.Fatal(got)
	}
	if z := x.Stats(); z.Entries != 32 || z.NextRevision != 33 {
		t.Fatalf("%+v", z)
	}
}
