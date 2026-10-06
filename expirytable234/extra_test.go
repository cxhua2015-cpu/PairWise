package expirytable234

import (
	"errors"
	"sync"
	"testing"
)

func TestOptionsAndKeyValidation(t *testing.T) {
	if _, err := New(Options{MaxEntries: 0, MaxKeyBytes: 4}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := New(Options{MaxEntries: 1, MaxKeyBytes: -1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	bad := []string{"", "Abc", "a b", "waytoolong", "a.b", "键"}
	for _, k := range bad {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 9}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, err)
		}
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Put, "a-z_0", 9}}}); err != nil {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 9}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Now: 5, Ops: []Op{{Put, "a", 5}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestRollbackOnCapacityAndNotFound(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	r, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}, {Put, "b", 10}}})
	if err != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, err)
	}
	before := x.Snapshot()
	// Over capacity after ops: everything must roll back.
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 10}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	// Touch of a missing key must roll back too.
	if _, err := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "zz", 10}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != 2 {
		t.Fatalf("state changed: %+v -> %+v", before, after)
	}
	// Time moved backwards.
	if _, err := x.Apply(Batch{Now: 0, Ops: []Op{{Put, "c", 9}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(0); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	r, err := x.Apply(Batch{Now: 2})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if x.Snapshot().Now != 2 {
		t.Fatal("empty batch should still advance now")
	}
	r, _ = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
}

func TestCandidatePurgeBeforeOps(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); err != nil {
		t.Fatal(err)
	}
	// "a" expires at Now=5, freeing capacity for "b" in the same batch.
	if _, err := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s.Entries)
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
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				_ = x.ValidateBatch(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
			}
			_, _ = x.Expire(50)
			c, err := x.Clone()
			if err == nil {
				_ = c.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	z := x.Stats()
	if z.Entries != len(s.Entries) || z.Now != s.Now || z.NextRevision != s.NextRevision {
		t.Fatalf("stats/snapshot disagree: %+v vs %+v", z, s)
	}
}
