package expirytable294

import (
	"errors"
	"sync"
	"testing"
)

func TestBoundaryValidation(t *testing.T) {
	x := table(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "A", 1}}},
		{Ops: []Op{{Put, "a b", 1}}},
		{Ops: []Op{{Put, "toolongkey", 1}}},
		{Ops: []Op{{Put, "a", 0}}},
		{Now: 5, Ops: []Op{{Touch, "a", 5}}},
		{Ops: []Op{{Delete, "bad key", 0}}},
	}
	for i, b := range cases {
		if err := x.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if err := x.ValidateBatch(Batch{Now: 1, Ops: []Op{{Put, "ok_key-1", 2}}}); err != nil {
		t.Fatal(err)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}})
	if err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// Would expire nothing but exceed final capacity; must roll back fully.
	_, err = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 100}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != r.Generation || len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("not rolled back: %+v", after)
	}
}

func TestRollbackNotFound(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	before := x.Snapshot()
	// Touch of missing key after a Put must roll back the Put, expiry and clock.
	_, err := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Touch, "ghost", 9}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if got := x.Snapshot(); got.Now != before.Now || got.NextRevision != before.NextRevision ||
		len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestExpireBoundaryAndTime(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	gone, err := x.Expire(5)
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(err, gone)
	}
	if _, err := x.Expire(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, err := x.Apply(Batch{Now: 2})
	if err != nil || r.Generation != 0 || x.Snapshot().Now != 2 {
		t.Fatal(r, err)
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
				_ = x.Stats()
				_ = x.Snapshot()
				if c, err := x.Clone(); err == nil {
					_ = c.Stats()
				}
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries > 128 || s.NextRevision < 2 {
		t.Fatalf("%+v", s)
	}
}
