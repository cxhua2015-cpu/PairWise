package resourcelease089

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

func TestInvalidInput(t *testing.T) {
	x := table(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Bad", 1}}},
		{Ops: []Op{{Put, "a b", 1}}},
		{Ops: []Op{{Put, "a/b", 1}}},
		{Ops: []Op{{Put, "toolongkey", 1}}},
		{Ops: []Op{{Put, "a", -1}}},
	}
	for _, b := range cases {
		if _, e := x.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	if s := x.Snapshot(); s.Generation != 0 || len(s.Entries) != 0 {
		t.Fatalf("state changed: %+v", s)
	}
}

func TestValidKeyCharset(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a-z_09", 10}}}); e != nil {
		t.Fatal(e)
	}
}

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 4}, {Put, "b", 5}}})
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	// ExpiresAt == Now is evicted by the next Apply as well.
	r, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	for _, en := range x.Snapshot().Entries {
		if en.Key == "b" {
			t.Fatal("b should have been evicted at closed boundary")
		}
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}})
	before := x.Snapshot()
	// Put a second key then overflow with a third: full rollback expected.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 100}, {Put, "c", 100}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("not rolled back: %+v", after)
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 10}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 1, Ops: []Op{{Touch, "ghost", 5}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != before.Now || got.Generation != before.Generation || got.NextRevision != before.NextRevision {
		t.Fatal("not rolled back")
	}
}

func TestTouchExpiredIsNotFound(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}}})
	// At Now=3 the entry is evicted in the candidate before ops run.
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoChange(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.Now != 0 || s.Generation != 0 {
		t.Fatalf("empty batch changed state: %+v", s)
	}
}

func TestExpireMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Expire(5); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 12345
	if x.Snapshot().Entries[0].ExpiresAt != 9 {
		t.Fatal("snapshot aliases internal state")
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
			for n := int64(1); n <= 20; n++ {
				if _, e := x.Apply(Batch{Ops: []Op{{Put, k, n + 100}}}); e != nil {
					t.Error(e)
					return
				}
				if _, e := x.Apply(Batch{Ops: []Op{{Touch, k, n + 200}}}); e != nil {
					t.Error(e)
					return
				}
				_, _ = x.Expire(0)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 32 {
		t.Fatal(len(s.Entries))
	}
	if s.NextRevision != 1+32*20*2 {
		t.Fatalf("revision leaked on rollback: %d", s.NextRevision)
	}
}
