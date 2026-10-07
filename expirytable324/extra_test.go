package expirytable324

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

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "toolongkey"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	good := []string{"a", "z0-_", "12345678"}
	for _, k := range good {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 100}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	x := table(t)
	for _, k := range []Kind{0, 4, 255} {
		if _, e := x.Apply(Batch{Ops: []Op{{Kind: k, Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: %v", k, e)
		}
	}
}

func TestTimeMonotone(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// Equal Now is allowed.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEvictionOnApply(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "old", 2}, {Put, "new", 100}}}); e != nil {
		t.Fatal(e)
	}
	// Advancing Now to 2 evicts "old" (closed interval) before ops run.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "new", 200}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "new" {
		t.Fatal(s.Entries)
	}
	// Evicted key can be Touched nowhere: it is gone.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "old", 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, e := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 50}, {Put, "b", 60}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Would end with 3 entries: must fail and roll back everything,
	// including the revision allocated for "c".
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 70}, {Delete, "a", 0}, {Put, "d", 80}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if after := x.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 20}, {Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if after := x.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	if s := x.Snapshot(); s.Generation != 0 || s.NextRevision != 1 {
		t.Fatal(s)
	}
	// Empty batch changes nothing.
	if r, e := x.Apply(Batch{Now: 3}); e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if s := x.Snapshot(); s.Generation != 0 || s.Now != 0 {
		t.Fatal(s)
	}
	r, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}, {Delete, "b", 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	if s := x.Snapshot(); s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	// Failed batch does not bump generation.
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Touch, "nope", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 4}, {Put, "b", 5}, {Put, "c", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
	gone, e = x.Expire(5)
	if e != nil || len(gone) != 0 {
		t.Fatal(e, gone)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot shares state")
	}
	gone, _ := x.Expire(9)
	gone[0].Key = "mutated"
	_ = gone
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 1000}}})
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
}
