package expirytable359

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "toolongkey", "é"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}, {Put, "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
}

func TestValidKeyCharset(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a-z0_9", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}, {Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.Now != 0 || len(s.Entries) != 0 || s.NextRevision != 1 || s.Generation != 0 {
		t.Fatalf("rollback leaked: %+v", s)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=3, but the batch still overflows -> full rollback.
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.Now != 1 || len(s.Entries) != 1 || s.Entries[0].Key != "a" || s.NextRevision != 2 || s.Generation != 1 {
		t.Fatalf("rollback leaked: %+v", s)
	}
}

func TestExpiryEvictionOnApply(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 2}}})
	// Now=2 evicts "a" (closed bound), freeing capacity for "b".
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	if k := x.Snapshot().Entries[0].Key; k != "b" {
		t.Fatal(k)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 5})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Now != 5 {
		t.Fatal("empty batch must still advance time")
	}
	r, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
}

func TestExpireMonotonicAndBoundary(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}})
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(2); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, _ = x.Expire(4)
	if len(gone) != 1 || gone[0].Key != "b" {
		t.Fatal(gone)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "zz"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
	gone, _ := x.Expire(9)
	gone[0].Key = "zz"
	if len(x.Snapshot().Entries) != 0 {
		t.Fatal("expire result aliases internal state")
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
			for j := int64(1); j <= 20; j++ {
				_, _ = x.Apply(Batch{Now: j, Ops: []Op{{Put, k, j + 100}}})
				_, _ = x.Apply(Batch{Now: j, Ops: []Op{{Touch, k, j + 200}}})
				_, _ = x.Expire(j)
				_ = x.Snapshot()
			}
			_, _ = x.Apply(Batch{Now: 500, Ops: []Op{{Delete, k, 0}}})
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal("capacity violated")
	}
}
