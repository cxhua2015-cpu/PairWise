package certificatelease

import (
	"errors"
	"fmt"
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
	bad := []string{"", "A", "a b", "a.b", "a/b", "toolongkey", "é"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	for _, k := range []string{"a", "z-0_9", "12345678"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 100}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 0, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 99, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
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

func TestEvictionBeforeOps(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at 2 <= Now 2, so it is evicted first and "b" fits.
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" || s.Now != 2 {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	// Eviction of "a" plus revision allocation must roll back with the failure.
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Generation != b.Generation || got.Now != b.Now ||
		got.NextRevision != b.NextRevision || len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatal(got)
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}, {Delete, "zz", 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	got := x.Snapshot()
	if got.Now != b.Now || got.NextRevision != b.NextRevision || got.Entries[0].ExpiresAt != 5 {
		t.Fatal(got)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 || x.Snapshot().Now != 7 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 8, Ops: []Op{{Put, "a", 10}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
}

func TestTimeMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e) // equal is allowed
	}
	if _, e := x.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(6); e != nil || x.Snapshot().Now != 6 {
		t.Fatal(e)
	}
}

func TestExpireBoundaryAndOrder(t *testing.T) {
	x := table(t)
	_, e := x.Apply(Batch{Ops: []Op{{Put, "b", 4}, {Put, "a", 4}, {Put, "c", 5}}})
	if e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
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
	s.Entries[0].Key = "zz"
	s.Entries[0].ExpiresAt = 1
	again := x.Snapshot()
	if again.Entries[0].Key != "a" || again.Entries[0].ExpiresAt != 9 {
		t.Fatal(again.Entries[0])
	}
	if again.NextRevision != 2 {
		t.Fatal(again.NextRevision)
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
			for j := int64(1); j <= 10; j++ {
				// ErrTime is expected when goroutines interleave clocks.
				if _, err := x.Apply(Batch{Now: j, Ops: []Op{{Put, k, j + 50}}}); err != nil && !errors.Is(err, ErrTime) {
					t.Error(err)
					return
				}
				if _, err := x.Apply(Batch{Now: j, Ops: []Op{{Touch, k, j + 60}}}); err != nil && !errors.Is(err, ErrTime) && !errors.Is(err, ErrNotFound) {
					t.Error(err)
					return
				}
				_ = x.Snapshot()
			}
		}()
	}
	w.Add(1)
	go func() {
		defer w.Done()
		for j := int64(1); j <= 10; j++ {
			_, _ = x.Expire(j)
		}
	}()
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 32 {
		t.Fatal(len(s.Entries))
	}
	seen := map[uint64]bool{}
	for _, e := range s.Entries {
		if e.Revision == 0 || e.ExpiresAt <= s.Now {
			t.Fatal(e)
		}
		if seen[e.Revision] {
			t.Fatal("duplicate revision", e.Revision)
		}
		seen[e.Revision] = true
	}
}
