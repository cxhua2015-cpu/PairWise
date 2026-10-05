package membershiplease

import (
	"errors"
	"fmt"
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
			t.Fatalf("%q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndNegativeValues(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestValidationBeforeTime(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "bad?", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "a", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 3}, {Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Now != b.Now || s.NextRevision != b.NextRevision || len(s.Entries) != 0 {
		t.Fatalf("state mutated: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	if e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	// Expiry of "a" at Now=2 would make room, but Now=1 keeps it.
	if _, e = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Now != b.Now || s.NextRevision != b.NextRevision || s.Generation != b.Generation || len(s.Entries) != 1 || s.Entries[0].Key != "a" {
		t.Fatalf("state mutated: %+v", s)
	}
	// Expiry at Now=2 frees the slot.
	if _, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	if x.Snapshot().Generation != r.Generation+1 {
		t.Fatal(x.Snapshot())
	}
}

func TestExpiryInsideBatch(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 9}}})
	r, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "b", 10}}})
	if e != nil || r.Revision != 3 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" || s.Entries[0].ExpiresAt != 10 {
		t.Fatalf("%+v", s)
	}
}

func TestEmptyBatch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.Generation != 0 || s.Now != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestExpireMonotonicAndBoundary(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 4}, {Put, "b", 5}}})
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if _, e = x.Expire(3); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if len(x.Snapshot().Entries) != 1 {
		t.Fatal(x.Snapshot())
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "zz"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
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
			k := fmt.Sprintf("k%02d", i)
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Expire(n)
				s := x.Snapshot()
				for _, e := range s.Entries {
					if e.ExpiresAt < 0 || e.Revision == 0 {
						t.Error("bad entry", e)
					}
				}
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 || s.NextRevision < s.Generation {
		t.Fatalf("%+v", s)
	}
}
