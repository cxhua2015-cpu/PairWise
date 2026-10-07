package expirytable414

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestValidationBoundaries(t *testing.T) {
	x := table(t)
	cases := []Batch{
		{Now: -1, Ops: []Op{{Put, "a", 1}}},                    // negative now
		{Ops: []Op{{Kind(0), "a", 1}}},                         // unknown kind
		{Ops: []Op{{Kind(9), "a", 1}}},                         // unknown kind
		{Ops: []Op{{Put, "", 1}}},                              // empty key
		{Ops: []Op{{Put, "A", 1}}},                             // uppercase
		{Ops: []Op{{Put, "a b", 1}}},                           // space
		{Ops: []Op{{Put, "é", 1}}},                             // non-ASCII
		{Ops: []Op{{Put, "123456789", 1}}},                     // too long
		{Ops: []Op{{Put, "a", -1}}},                            // negative expiry
		{Ops: []Op{{Put, "a", 0}}},                             // expiry <= now
		{Ops: []Op{{Touch, "a", 0}}},                           // touch expiry <= now
	}
	for i, b := range cases {
		if err := x.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: got %v", i, err)
		}
		if _, err := x.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: got %v", i, err)
		}
	}
	if s := x.Snapshot(); s.Generation != 0 || s.Now != 0 || len(s.Entries) != 0 || s.NextRevision != 1 {
		t.Fatalf("failed validation mutated state: %+v", s)
	}
	good := Batch{Ops: []Op{{Put, "k_-9", 1}, {Touch, "k_-9", 2}, {Delete, "k_-9", 0}}}
	if err := x.ValidateBatch(good); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, err)
		}
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 5}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// Batch that expires entries, allocates revisions, then fails on capacity.
	full, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	_, _ = full.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}})
	snap := full.Snapshot()
	_, err := full.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := full.Snapshot(); got.Now != snap.Now || got.NextRevision != snap.NextRevision ||
		got.Generation != snap.Generation || len(got.Entries) != 2 {
		t.Fatalf("capacity failure not rolled back: %+v -> %+v", snap, got)
	}
	_ = before
}

func TestExpireBoundaryAndClock(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	gone, err := x.Expire(5)
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(err, gone)
	}
	if _, err := x.Expire(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if s := x.Stats(); s.Now != 5 || s.Entries != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	x := table(t)
	r, err := x.Apply(Batch{Now: 3})
	if err != nil || r.Generation != 0 || x.Snapshot().Now != 3 {
		t.Fatal(r, err)
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
			for n := int64(1); n <= 10; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				_ = x.ValidateBatch(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
			}
			_, _ = x.Expire(50)
			c, err := x.Clone()
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = c.Expire(200)
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if s.Generation == 0 || s.NextRevision < 2 {
		t.Fatalf("%+v", s)
	}
}

func TestCloneIndependence(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	c, _ := x.Clone()
	if _, err := c.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	if len(x.Snapshot().Entries) != 1 || len(c.Snapshot().Entries) != 2 {
		t.Fatal("clone shares state with original")
	}
	if x.Stats().Now != 1 || c.Stats().Now != 2 {
		t.Fatal("clocks not independent")
	}
}
