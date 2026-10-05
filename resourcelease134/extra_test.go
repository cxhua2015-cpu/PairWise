package resourcelease134

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestBoundaryValidationAndCapacityRollback(t *testing.T) {
	if _, e := New(Options{MaxEntries: 0, MaxKeyBytes: 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x := table(t)
	before := x.Snapshot()
	for _, b := range []Batch{
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Upper", 1}}},
		{Ops: []Op{{Put, "way-too-long-key", 1}}},
		{Ops: []Op{{Put, "a", -1}}},
		{Now: -1, Ops: []Op{{Put, "a", 1}}},
	} {
		if _, e := x.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("invalid batches mutated state")
	}
	// Fill to capacity, then a failing batch must roll back expiry+time+revision.
	x2, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x2.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	mid := x2.Snapshot()
	// "a" expires at Now=2, but the batch overfills and must roll back.
	_, e := x2.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x2.Snapshot(); !reflect.DeepEqual(mid, got) {
		t.Fatalf("capacity failure not rolled back: %+v", got)
	}
	// NotFound also rolls back fully.
	_, e = x2.Apply(Batch{Now: 3, Ops: []Op{{Touch, "missing", 5}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(mid, x2.Snapshot()) {
		t.Fatal(e)
	}
}

func TestEmptyBatchAndMonotonicTime(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
	if _, e := x.Apply(Batch{Now: 6, Ops: []Op{{Put, "a", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(6); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	p, _ := NewPolicy(2, []string{"alice"})
	c, _ := NewCoordinator(x, p)
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%d", i)
			_, _ = c.Apply("alice", Batch{Ops: []Op{{Put, k, 100}}})
			_, _ = c.Apply("mallory", Batch{Ops: []Op{{Put, k, 100}}})
			_, _ = x.Expire(int64(i % 3))
			_ = x.Snapshot()
			_ = c.Decisions()
			_ = p.ReplaceActors([]string{"alice", "bob"})
		}()
	}
	w.Wait()
	ds := c.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("gap in audit sequence at %d: %+v", i, d)
		}
	}
	if len(x.Snapshot().Entries) > 128 {
		t.Fatal("capacity exceeded")
	}
}
