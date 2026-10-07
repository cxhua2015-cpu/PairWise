package expirytable304

import (
	"errors"
	"reflect"
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
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey", "中文"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrTime) {
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
	if _, e := x.Expire(5); e != nil {
		t.Fatal(e)
	}
}

func TestNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "a", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	before := x.Snapshot()
	// "a" would be evicted at Now=2, but the batch still overflows and must roll back fully.
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
}

func TestEvictionOnApply(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 5}}})
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "c", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatal(s.Entries)
	}
	if s.Now != 3 || s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
}

func TestEmptyBatch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.Generation != 0 || s.Now != 0 {
		t.Fatal(s)
	}
}

func TestExpireBoundaryAndIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}})
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	if s := x.Snapshot(); len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s.Entries)
	}
	ent := x.Snapshot().Entries
	ent[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "b" {
		t.Fatal("snapshot shares internal state")
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
			for n := int64(0); n < 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 || s.NextRevision < 2 {
		t.Fatal(s)
	}
}
