package resourcelease169

import (
	"errors"
	"fmt"
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
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a/b"}
	for _, k := range bad {
		_, e := x.Apply(Batch{Ops: []Op{{Put, k, 9}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefgh"}
	for _, k := range good {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 99}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKindAndNegativeTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -2}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExpiryBoundaryInclusive(t *testing.T) {
	x := table(t)
	// Entry expiring at Now is removed before ops run, so Put of the
	// same key in the same batch recreates it.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	r, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 50}}})
	if e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "a" || s.Entries[0].ExpiresAt != 50 {
		t.Fatal(s.Entries)
	}
	if s.Entries[0].Revision != r.Revision {
		t.Fatal(s.Entries[0], r)
	}
	gone, e := x.Expire(6)
	if e != nil || len(gone) != 1 || gone[0].Key != "b" {
		t.Fatal(e, gone)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// "a" would expire at Now=2, but the batch still overflows and must
	// roll back the expiration, the clock, and the revision counter.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatalf("state changed after rollback: %+v", x.Snapshot())
	}
}

func TestRollbackOnNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "ghost", 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after rollback")
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Delete, "a", 0}}}); e != nil {
		t.Fatal(e)
	}
	if g := x.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestTimeMonotonicAcrossMethods(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 7, Ops: []Op{{Put, "a", 100}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(6); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(8); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 7, Ops: []Op{{Put, "b", 100}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if n := x.Snapshot().Now; n != 8 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mut"
	s.Entries[0].ExpiresAt = 0
	again := x.Snapshot()
	if again.Entries[0].Key != "a" || again.Entries[0].ExpiresAt != 9 {
		t.Fatal("snapshot shares memory with table")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 40}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 40}}})
				_, _ = x.Expire(n)
				s := x.Snapshot()
				for _, e := range s.Entries {
					if e.Revision == 0 || e.Revision >= s.NextRevision {
						t.Error("bad revision", e, s.NextRevision)
						return
					}
				}
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) == 0 || s.Generation == 0 {
		t.Fatal(s)
	}
}

func TestConcurrentExpireSnapshot(t *testing.T) {
	x, _ := New(Options{MaxEntries: 64, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 10}, {Put, "b", 20}}}); e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for n := int64(0); n < 30; n++ {
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	if len(x.Snapshot().Entries) != 0 {
		t.Fatal(x.Snapshot().Entries)
	}
}
