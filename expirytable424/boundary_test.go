package expirytable424

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatalf("gone=%v err=%v", gone, e)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestApplySweepsAtBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// ExpiresAt == Now must be swept before the ops run.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatalf("%+v", s)
	}
}

func TestCapacityRollbackRestoresEvictions(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// "a" is swept (ExpiresAt 2 <= Now 2), then two puts overflow capacity 1:
	// the whole batch, including the sweep and clock, must roll back.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatalf("state changed: %+v", x.Snapshot())
	}
}

func TestNotFoundRollbackRestoresRevision(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	if e != nil || r.Revision != 1 {
		t.Fatal(e, r)
	}
	before := x.Snapshot()
	_, e = x.Apply(Batch{Ops: []Op{{Put, "b", 9}, {Touch, "missing", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("revision/state leaked after rollback")
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 1 {
		t.Fatalf("empty batch changed generation: %+v", r)
	}
	// Delete of an absent key is a no-op: generation unchanged.
	r, e = x.Apply(Batch{Now: 1, Ops: []Op{{Delete, "nope", 0}}})
	if e != nil || r.Generation != 1 {
		t.Fatalf("no-op delete changed generation: %+v", r)
	}
}

func TestKeyValidationBoundaries(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 3})
	cases := []struct {
		key string
		ok  bool
	}{
		{"abc", true},
		{"a1-_", false}, // over byte limit
		{"", false},
		{"Abc", false},
		{"a b", false},
		{"a.b", false},
		{"é", false},
		{"z9_", true},
	}
	for _, c := range cases {
		e := x.ValidateBatch(Batch{Ops: []Op{{Put, c.key, 9}}})
		if c.ok && e != nil {
			t.Fatalf("key %q: %v", c.key, e)
		}
		if !c.ok && !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: got %v", c.key, e)
		}
	}
}

func TestUnknownKindAndNegativeTime(t *testing.T) {
	x := table(t)
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind: 99, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Now: -1}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
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
			k := string(rune('a' + i))
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
				_ = x.Stats()
				_, _, _, _ = x.Preview(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				if n%5 == 0 {
					c, err := x.Clone()
					if err == nil {
						_ = c.Snapshot()
					}
				}
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries < 0 || s.NextRevision < 1 {
		t.Fatalf("%+v", s)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "corrupt"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
	gone, e := x.Expire(9)
	if e != nil || len(gone) != 1 {
		t.Fatal(e, gone)
	}
	gone[0].Key = "corrupt"
	if len(x.Snapshot().Entries) != 0 {
		t.Fatal("expire result aliases internal state")
	}
}
