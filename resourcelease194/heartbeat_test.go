package resourcelease194

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {3, 0}, {-1, 8}, {3, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "toolongkey", "中文"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}, {Put, "b", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); len(s.Entries) != 1 {
		t.Fatal(s.Entries)
	}
}

func TestNegativeAndStaleTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Now != 5 || len(s.Entries) != 1 {
		t.Fatal(s)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Failed batch must roll back expiry, time and revision too.
	_, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 4}}})
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 10, Ops: []Op{{Put, "b", 99}, {Touch, "ghost", 1}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal(e, x.Snapshot())
	}
	if len(x.Snapshot().Entries) != 1 {
		t.Fatal("expiry from failed batch leaked")
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 2}}})
	b := x.Snapshot()
	// Expiry frees both slots, but the final size still exceeds capacity.
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}, {Put, "e", 9}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal(e, x.Snapshot())
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r, _ := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Ops: []Op{{Touch, "a", 9}, {Delete, "a", 0}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Now: 1}) // empty batch: generation unchanged
	if r.Generation != 2 || r.Revision != 0 {
		t.Fatal(r)
	}
	if s := x.Snapshot(); s.Generation != 2 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestExpireClosedBoundaryAndIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}})
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s.Entries)
	}
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "b" {
		t.Fatal("snapshot shares state")
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
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 || s.NextRevision < s.Generation {
		t.Fatal(s.Generation, s.NextRevision, len(s.Entries))
	}
}
