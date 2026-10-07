package expirytable439

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {3, 0}, {-1, 8}, {3, -1}} {
		if _, err := New(o); err != ErrInvalidOptions {
			t.Fatalf("opts %+v: %v", o, err)
		}
	}
}

func TestEmptyBatchNoop(t *testing.T) {
	x := table(t)
	r, err := x.Apply(Batch{Now: 7})
	if err != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, err)
	}
	if s := x.Snapshot(); s.Now != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("empty batch mutated state: %+v", s)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "a", 5}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	if _, err := x.Apply(Batch{Ops: []Op{{Touch, "missing", 9}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Delete, "missing", 0}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "b", 9}, {Delete, "missing", 0}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("state changed after failed batch: %+v", got)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	_, err := x.Apply(Batch{Ops: []Op{{Put, "c", 9}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("capacity failure leaked state: %+v", got)
	}
}

func TestExpiryEvictionInsideBatch(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "old", 2}}}); err != nil {
		t.Fatal(err)
	}
	r, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "new", 9}}})
	if err != nil || r.Generation != 2 {
		t.Fatal(r, err)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "new" {
		t.Fatalf("expired entry not evicted first: %+v", s)
	}
}

func TestKeyBoundaries(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 3})
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "a_1", 5}, {Put, "b-2", 5}}}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"", "abcd", "A", "a b", "a.b", "é"} {
		if _, err := x.Apply(Batch{Ops: []Op{{Put, k, 5}}}); err != ErrInvalidInput {
			t.Fatalf("key %q: %v", k, err)
		}
	}
}

func TestExpireClosedBoundaryAndClock(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}}); err != nil {
		t.Fatal(err)
	}
	gone, err := x.Expire(3)
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(gone, err)
	}
	if _, err := x.Expire(2); err != ErrTime {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 0, Ops: []Op{{Put, "c", 9}}}); err != ErrTime {
		t.Fatal(err)
	}
}

func TestPreviewDoesNotConsumeClock(t *testing.T) {
	x := table(t)
	if _, _, _, err := x.Preview(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	if s := x.Snapshot(); s.Now != 0 || s.Generation != 0 || len(s.Entries) != 0 {
		t.Fatalf("preview leaked: %+v", s)
	}
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
}

func TestCloneIsolationAndClock(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot(); !reflect.DeepEqual(got, x.Snapshot()) {
		t.Fatalf("clone diverged: %+v", got)
	}
	if _, err := c.Apply(Batch{Now: 3, Ops: []Op{{Delete, "a", 0}}}); err != nil {
		t.Fatal(err)
	}
	if len(x.Snapshot().Entries) != 1 || x.Snapshot().Now != 2 {
		t.Fatal("clone mutated original")
	}
	if _, err := c.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}}); err != ErrTime {
		t.Fatalf("clone lost logical clock: %v", err)
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
			k := string(rune('a' + i%26))
			_, _ = x.Apply(Batch{Ops: []Op{{Put, k, 50}}})
			_, _ = x.Expire(int64(i % 3))
			_, _, _, _ = x.Preview(Batch{Ops: []Op{{Touch, k, 60}}})
			_, _ = x.Clone()
			_ = x.Stats()
			_ = x.Snapshot()
			_ = x.ValidateBatch(Batch{Ops: []Op{{Put, k, 70}}})
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 26 {
		t.Fatal(len(s.Entries))
	}
}
