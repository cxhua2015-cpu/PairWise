package sessiontable

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestInvalidKeysAndKind(t *testing.T) {
	x := table(t)
	bad := []Op{
		{Put, "", 1},
		{Put, "Upper", 1},
		{Put, "has space", 1},
		{Put, "dot.key", 1},
		{Put, "waytoolong", 1},
		{Put, "中文", 1},
		{Kind(0), "a", 1},
		{Kind(99), "a", 1},
	}
	for _, op := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 1000}, {Put, "b", 1000}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	before := x.Snapshot()
	// Batch ends over capacity: must roll back time and revisions too.
	_, e = x.Apply(Batch{Now: 200, Ops: []Op{{Put, "c", 300}, {Put, "d", 300}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("state changed after failed batch: %+v", got)
	}
	// Error mid-batch (NotFound) also rolls back everything.
	_, e = x.Apply(Batch{Now: 200, Ops: []Op{{Put, "c", 300}, {Touch, "ghost", 1}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("state changed after failed batch: %+v", got)
	}
}

func TestExpiryBeforeOps(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=5 (closed boundary), freeing the slot for "b".
	r, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" || s.Now != 5 {
		t.Fatalf("%+v", s)
	}
	// Touch of an entry expired in the same batch fails.
	if _, e := x.Apply(Batch{Now: 9, Ops: []Op{{Touch, "b", 10}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 0})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 3}}}); e != nil {
		t.Fatal(e)
	}
	r, e = x.Apply(Batch{})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
	if g := x.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestTimeMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestExpireClosedBoundaryAndOrder(t *testing.T) {
	x := table(t)
	_, e := x.Apply(Batch{Ops: []Op{{Put, "c", 4}, {Put, "a", 4}, {Put, "b", 5}}})
	if e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "c" {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	s.Entries[0].ExpiresAt = -1
	got := x.Snapshot()
	if got.Entries[0].Key != "a" || got.Entries[0].ExpiresAt != 9 {
		t.Fatalf("snapshot aliases internal state: %+v", got.Entries[0])
	}
	gone, _ := x.Expire(9)
	gone[0].Key = "mutated"
	if k := x.Snapshot(); len(k.Entries) != 0 {
		t.Fatal(k)
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
		t.Fatal(len(s.Entries))
	}
	for _, e := range s.Entries {
		if e.Revision == 0 || e.Revision >= s.NextRevision {
			t.Fatalf("bad revision %+v next=%d", e, s.NextRevision)
		}
	}
}
