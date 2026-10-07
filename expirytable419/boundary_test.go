package expirytable419

import (
	"errors"
	"sync"
	"testing"
)

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyCharsetAndLength(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "toolongkey", "é"}
	for _, k := range bad {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "ab-c_d09", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	x := table(t)
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind: Kind(0), Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestDeadOnArrivalRejected(t *testing.T) {
	x := table(t)
	if e := x.ValidateBatch(Batch{Now: 3, Ops: []Op{{Put, "a", 3}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Now: 3, Ops: []Op{{Touch, "a", 2}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestRollbackOnNotFoundAndCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}, {Put, "b", 10}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Touch of missing key must roll back the whole batch including expiry.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Kind: Delete, Key: "a"}, {Touch, "zz", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != 1 || got.Generation != before.Generation || len(got.Entries) != 2 {
		t.Fatalf("state changed after failed batch: %+v", got)
	}
	// Overflowing final capacity must roll back everything.
	if _, e := x.Apply(Batch{Now: 6, Ops: []Op{{Put, "c", 10}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != 1 || len(got.Entries) != 2 {
		t.Fatalf("capacity failure leaked: %+v", got)
	}
}

func TestExpiryOnApplyAndEmptyBatch(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" {
		t.Fatalf("closed-boundary expiry failed: %+v", s.Entries)
	}
	g := s.Generation
	if _, e := x.Apply(Batch{Now: 3}); e != nil {
		t.Fatal(e)
	}
	if x.Snapshot().Generation != g {
		t.Fatal("empty batch bumped generation")
	}
}

func TestExpireMonotonicAndBoundary(t *testing.T) {
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
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	c, e := x.Clone()
	if e != nil {
		t.Fatal(e)
	}
	cs, xs := c.Stats(), x.Stats()
	if cs != xs {
		t.Fatalf("clocks diverged: %+v vs %+v", cs, xs)
	}
	_, _ = c.Apply(Batch{Now: 3, Ops: []Op{{Kind: Delete, Key: "a"}}})
	if len(x.Snapshot().Entries) != 1 || len(c.Snapshot().Entries) != 0 {
		t.Fatal("clone not isolated")
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
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				_ = x.ValidateBatch(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 16 || s.Now != 50 {
		t.Fatalf("entries=%d now=%d", len(s.Entries), s.Now)
	}
	// Returned slices must be isolated from internal state.
	s.Entries[0].ExpiresAt = -1
	if x.Snapshot().Entries[0].ExpiresAt == -1 {
		t.Fatal("snapshot aliases internal state")
	}
}
