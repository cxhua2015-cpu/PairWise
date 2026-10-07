package expirytable329

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey", "中文"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
}

func TestCandidateExpiryClosedBoundary(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	// ExpiresAt <= Now removed inside Apply before ops run.
	r, e := x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "b", 9}}})
	if e != nil || r.Revision != 3 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" || s.Entries[0].ExpiresAt != 9 {
		t.Fatal(s.Entries)
	}
	// Touch of an entry expired by the same batch fails and rolls back.
	if _, e := x.Apply(Batch{Now: 9, Ops: []Op{{Touch, "b", 10}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Rolled back: time and entries unchanged.
	if got := x.Snapshot(); got.Now != 5 || len(got.Entries) != 1 || got.Entries[0].Key != "b" {
		t.Fatal(got)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// "a" expired in the candidate but the failure must roll back expiry,
	// time, revisions and generation.
	if got := x.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("before=%+v after=%+v", before, got)
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Delete, "zz", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatal(got)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "zz", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	g0 := x.Snapshot().Generation
	if _, e := x.Apply(Batch{Now: 1}); e != nil {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Generation != g0 || got.Now != 1 {
		t.Fatal(got)
	}
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	if e != nil || r.Generation != g0+1 {
		t.Fatal(e, r)
	}
}

func TestExpireReturnsAndAdvances(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "b", 3}, {Put, "a", 3}, {Put, "c", 4}}})
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	if got := x.Snapshot(); got.Now != 3 || len(got.Entries) != 1 || got.Entries[0].Key != "c" {
		t.Fatal(got)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 0
	if got := x.Snapshot(); got.Entries[0].ExpiresAt != 9 {
		t.Fatal(got)
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
