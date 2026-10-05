package credentiallease

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
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a+b"}
	for _, k := range bad {
		_, e := x.Apply(Batch{Ops: []Op{{Put, k, 9}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 9}, {Kind(9), "a", 9}}[1:]}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestExpiryPurgeOnApply(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 10}}}); e != nil {
		t.Fatal(e)
	}
	// Now=3 purges "a" (ExpiresAt <= Now), leaving room for two more puts.
	r, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "c", 10}, {Put, "d", 10}}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Generation != 2 {
		t.Fatal(r.Generation)
	}
	s := x.Snapshot()
	if len(s.Entries) != 3 || s.Now != 3 {
		t.Fatal(s)
	}
	keys := []string{s.Entries[0].Key, s.Entries[1].Key, s.Entries[2].Key}
	if !reflect.DeepEqual(keys, []string{"b", "c", "d"}) {
		t.Fatal(keys)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	for _, ops := range [][]Op{
		{{Touch, "missing", 9}},
		{{Delete, "missing", 0}},
		{{Put, "b", 9}, {Touch, "ghost", 9}}, // rollback must undo the put
	} {
		if _, e := x.Apply(Batch{Now: 2, Ops: ops}); !errors.Is(e, ErrNotFound) {
			t.Fatal(ops, e)
		}
		if got := x.Snapshot(); !reflect.DeepEqual(b, got) {
			t.Fatalf("state changed after failed batch: %+v", got)
		}
	}
}

func TestCapacityRollback(t *testing.T) {
	x := table(t) // MaxEntries 3
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}, {Put, "c", 9}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	// Purge of "a" frees a slot, but 4 live entries still exceed capacity.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "d", 9}, {Put, "e", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(b, got) {
		t.Fatal("capacity failure must roll back purge, time and revisions")
	}
}

func TestEmptyBatchNoChange(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	s := x.Snapshot()
	if s.Generation != 0 || s.Now != 0 || len(s.Entries) != 0 {
		t.Fatal(s)
	}
}

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 4}, {Put, "b", 5}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	gone, e = x.Expire(4)
	if e != nil || len(gone) != 0 {
		t.Fatal(e, gone)
	}
	if x.Snapshot().Now != 4 {
		t.Fatal("Expire must advance time")
	}
}

func TestRevisionAllocation(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Touch, "a", 10}, {Delete, "a", 0}, {Put, "b", 9}}})
	if e != nil || r.Revision != 3 {
		t.Fatal(r, e)
	}
	s := x.Snapshot()
	if s.NextRevision != 4 || s.Entries[0].Revision != 3 {
		t.Fatal(s)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, e := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 100}}})
				_, _ = x.Expire(n)
				s := x.Snapshot()
				for _, en := range s.Entries {
					if en.ExpiresAt <= s.Now {
						t.Errorf("live entry %v expired at %d", en, s.Now)
					}
				}
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal("capacity exceeded")
	}
}
