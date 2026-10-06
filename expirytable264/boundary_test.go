package expirytable264

import (
	"errors"
	"sync"
	"testing"
)

func TestEvictionRolledBackOnCapacityFailure(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// "a" (ExpiresAt 2) is evicted on the candidate at Now=2, then Put "b"
	// and Put "c" overflow capacity: eviction, time and revision must roll back.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.Now != 1 || s.NextRevision != 2 || len(s.Entries) != 1 || s.Entries[0].Key != "a" {
		t.Fatalf("state not rolled back: %+v", s)
	}
}

func TestNotFoundRollsBackRevision(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 5}, {Touch, "ghost", 6}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.NextRevision != 1 || len(s.Entries) != 0 || s.Generation != 0 {
		t.Fatalf("revision/generation leaked: %+v", s)
	}
}

func TestExpireClosedIntervalAndMonotonic(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}})
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if _, e = x.Expire(2); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e = x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidateBatchStructuralRules(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Upper", 1}}},
		{Ops: []Op{{Put, "waytoolong", 1}}},
		{Ops: []Op{{Put, "a", 0}}},
		{Ops: []Op{{Touch, "a", 0}}},
		{Ops: []Op{{Delete, "bad key", 0}}},
	}
	for i, b := range cases {
		if e := x.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "ok-1_", 1}, {Delete, "ok-1_", 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 || x.Snapshot().Now != 3 {
		t.Fatal(e, r)
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	c, e := x.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != x.Stats() {
		t.Fatal("clone diverges at birth")
	}
	_, _ = c.Apply(Batch{Now: 3, Ops: []Op{{Delete, "a", 0}}})
	if x.Stats().Entries != 1 || c.Stats().Entries != 0 {
		t.Fatal("clone aliases original")
	}
	if _, e = c.Apply(Batch{Now: 4}); e != nil {
		t.Fatal("clone lost logical clock:", e)
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
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Expire(n)
				_ = x.Stats()
				_ = x.Snapshot()
				if n%10 == 0 {
					_, _ = x.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries > 16 || s.Now != 50 {
		t.Fatalf("%+v", s)
	}
}
