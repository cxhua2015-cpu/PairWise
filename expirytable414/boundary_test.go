package expirytable414

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	x := table(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Bad", 1}}},
		{Ops: []Op{{Put, "a b", 1}}},
		{Ops: []Op{{Put, "toolongkey", 1}}},
		{Ops: []Op{{Put, "a", -1}}},
		{Ops: []Op{{Put, "a", 0}}},
		{Now: 2, Ops: []Op{{Touch, "a", 2}}},
	}
	for i, b := range bad {
		if e := x.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := x.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	good := Batch{Now: 1, Ops: []Op{{Put, "a-z_0", 2}, {Delete, "a-z_0", 0}}}
	if e := x.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoop(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if s.Generation != 0 || s.Now != 0 || len(s.Entries) != 0 {
		t.Fatalf("empty batch mutated state: %+v", s)
	}
}

func TestRollbackNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Touch, "ghost", 9}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal(e)
	}
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "ghost", 0}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal(e)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal(e)
	}
	// Expiry inside the candidate frees capacity for the same batch.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	if got := x.Snapshot().Entries[0].Key; got != "b" {
		t.Fatal(got)
	}
}

func TestExpireBoundaryAndClock(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 4}, {Put, "b", 5}}})
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(3); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if x.Snapshot().Now != 4 {
		t.Fatal("clock not advanced")
	}
}

func TestCloneIndependence(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}}})
	c, e := x.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(x.Snapshot(), c.Snapshot()) {
		t.Fatal("clone diverges")
	}
	if _, e := c.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	if len(x.Snapshot().Entries) != 1 || x.Stats().Now != 3 {
		t.Fatal("clone write leaked into original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				if n%10 == 0 {
					_, _ = x.Expire(n)
				}
				if n%25 == 0 {
					_, _ = x.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries == 0 || s.Entries > 8 {
		t.Fatalf("stats: %+v", s)
	}
	if int(s.Generation) != 0 && s.NextRevision < s.Generation {
		t.Fatalf("revision clock behind generation: %+v", s)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 5}}})
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 999
	if x.Snapshot().Entries[0].ExpiresAt != 5 {
		t.Fatal("snapshot aliases internal state")
	}
}
