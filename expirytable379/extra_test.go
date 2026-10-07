package expirytable379

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -3}} {
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
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}, {Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(x.Snapshot().Entries) != 0 {
		t.Fatal("state mutated by invalid batches")
	}
}

func TestSweepBeforeOps(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=2 (closed bound), freeing capacity for "b".
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" || s.Now != 2 {
		t.Fatal(s)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}})
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 100}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("capacity failure must roll back sweep, time and revision")
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "zz", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "zz", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Revision allocated to earlier ops in the batch must roll back too.
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "b", 9}, {Touch, "zz", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("error must roll back revisions")
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r, _ := x.Apply(Batch{Ops: []Op{}})
	if r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}, {Touch, "a", 10}}})
	if r.Generation != 1 || r.Revision != 3 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Ops: []Op{{Delete, "b", 0}}})
	if r.Generation != 2 || r.Revision != 3 {
		t.Fatal(r)
	}
	s := x.Snapshot()
	if s.NextRevision != 4 || s.Entries[0].Revision != 3 {
		t.Fatal(s)
	}
}

func TestExpireMonotonicAndIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 5}}})
	if _, e := x.Expire(0); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	s := x.Snapshot()
	if s.Entries[0].Key != "b" {
		t.Fatal("returned slices must be isolated")
	}
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "b" {
		t.Fatal("snapshot must be isolated")
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
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal("capacity violated")
	}
}
