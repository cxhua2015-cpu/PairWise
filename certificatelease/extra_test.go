package certificatelease

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestKeyCharset(t *testing.T) {
	x, _ := New(Options{MaxEntries: 8, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a1-_b2", 9}}}); e != nil {
		t.Fatal(e)
	}
	for _, k := range []string{"", "A", "a b", "a.b", "abcdefghi", "中文"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 9}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestClosedIntervalSweep(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	r, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}}})
	if e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" || r.Generation != 2 {
		t.Fatal(s, r)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	before := x.Snapshot()
	// "a" would be swept at Now=2, but the batch then adds two entries and
	// exceeds capacity; eviction, time and revision must all roll back.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("rolled back mismatch: %+v vs %+v", before, after)
	}
}

func TestRollbackNotFound(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Touch, "missing", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != before.Now || got.NextRevision != before.NextRevision || len(got.Entries) != 1 {
		t.Fatalf("state changed on error: %+v", got)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Now != 3 {
		t.Fatal("empty batch must still advance now")
	}
}

func TestExpireBoundaryAndSort(t *testing.T) {
	x, _ := New(Options{MaxEntries: 8, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "c", 4}, {Put, "a", 4}, {Put, "b", 5}}})
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "c" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "b" {
		t.Fatal("snapshot not isolated")
	}
	if _, e := x.Expire(3); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNegativeTimeAndOptions(t *testing.T) {
	if _, e := New(Options{MaxEntries: 0, MaxKeyBytes: 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxEntries: 1, MaxKeyBytes: -1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 4})
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 0, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
			k := fmt.Sprintf("k%06d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Expire(n)
				s := x.Snapshot()
				for _, e := range s.Entries {
					_ = e.Revision
				}
			}
		}()
	}
	w.Wait()
}

func TestRevisionMonotonicAcrossRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r1, _ := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}}) // fails on capacity
	r2, _ := x.Apply(Batch{Ops: []Op{{Touch, "a", 10}}})
	if r2.Revision != r1.Revision+1 {
		t.Fatalf("revision leaked from failed batch: %d -> %d", r1.Revision, r2.Revision)
	}
}
