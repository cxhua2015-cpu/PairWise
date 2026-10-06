package expirytable229

import (
	"errors"
	"sync"
	"testing"
)

func TestOptionsAndInputValidation(t *testing.T) {
	if _, e := New(Options{0, 8}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{3, 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x := table(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 5}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 5}}},
		{Ops: []Op{{Put, "", 5}}},
		{Ops: []Op{{Put, "Upper", 5}}},
		{Ops: []Op{{Put, "toolongkey", 5}}},
		{Ops: []Op{{Put, "a", 0}}},              // dead on arrival
	}
	for i, b := range bad {
		if e := x.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := x.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	if s := x.Snapshot(); s.Generation != 0 || len(s.Entries) != 0 {
		t.Fatalf("state mutated: %+v", s)
	}
}

func TestNotFoundAndCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Touch of a missing key must roll back evictions, clock and revisions.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Final capacity overflow must roll back too.
	if _, e := x.Apply(Batch{Now: 6, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != before.Now || got.Generation != before.Generation ||
		got.NextRevision != before.NextRevision || len(got.Entries) != 2 {
		t.Fatalf("rollback failed: %+v vs %+v", got, before)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 5}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
	if _, e := x.Apply(Batch{Now: 1}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(0); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestExpireBoundaryAndIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "b" {
		t.Fatal("snapshot aliases returned slice")
	}
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 0
	if x.Snapshot().Entries[0].ExpiresAt != 6 {
		t.Fatal("snapshot aliases internal state")
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
			for n := int64(1); n <= 10; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				_ = x.ValidateBatch(Batch{Now: n, Ops: []Op{{Put, k, 100}}})
			}
		}()
	}
	w.Wait()
	st := x.Stats()
	if st.Entries != 16 || st.Now != 10 {
		t.Fatalf("%+v", st)
	}
	c, err := x.Clone()
	if err != nil || c.Stats() != st {
		t.Fatal(err, c.Stats(), st)
	}
}
