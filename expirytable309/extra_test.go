package expirytable309

import (
	"errors"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {3, 0}, {-1, 8}, {3, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
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
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ab-c_d9", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}, {Put, "b", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if got := len(x.Snapshot().Entries); got != 1 {
		t.Fatal(got)
	}
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "z", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "z", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if x.Snapshot().Now != 5 {
		t.Fatal(x.Snapshot().Now)
	}
}

func TestEvictBeforeOps(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at 2 <= Now=2, so Touch must fail and roll back the Put of "b".
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Touch, "a", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.Now != 1 || s.Generation != 1 || len(s.Entries) != 1 || s.Entries[0].Key != "a" {
		t.Fatal(s)
	}
	// Closed boundary: ExpiresAt == Now is evicted before ops run.
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s = x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "c" {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, e := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Now != b.Now || s.Generation != b.Generation ||
		s.NextRevision != b.NextRevision || len(s.Entries) != 1 || s.Entries[0].Key != "a" {
		t.Fatal(s)
	}
}

func TestDeleteMissingAndGeneration(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if g := x.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
	// Empty batch is a no-op for generation.
	if r, e := x.Apply(Batch{Now: 1}); e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if g := x.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestExpireClosedBoundaryAndOrder(t *testing.T) {
	x := table(t)
	_, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 2}, {Put, "c", 5}}})
	if e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 2 || gone[0].Key != "b" || gone[1].Key != "a" {
		t.Fatal(e, gone)
	}
	if len(x.Snapshot().Entries) != 1 {
		t.Fatal(x.Snapshot().Entries)
	}
	gone[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "c" {
		t.Fatal("returned slice aliases internal state")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 1
	if x.Snapshot().Entries[0].ExpiresAt != 9 {
		t.Fatal("snapshot aliases internal state")
	}
	if s.NextRevision != 2 {
		t.Fatal(s.NextRevision)
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
			k := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 100}}})
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
		if e.ExpiresAt <= s.Now {
			t.Fatalf("stale entry %+v at now=%d", e, s.Now)
		}
	}
}
