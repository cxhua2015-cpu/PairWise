package expirytable384

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustTable(t *testing.T, o Options) *Table {
	t.Helper()
	x, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 8, MaxKeyBytes: 4})
	bad := []string{"", "A", "a b", "abc!", "abcde", "é"}
	for _, k := range bad {
		if _, err := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, err)
		}
	}
	for _, k := range []string{"a", "z-9_", "abcd"} {
		if _, err := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); err != nil {
			t.Fatalf("key %q: %v", k, err)
		}
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 8, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Delete, "a", 3}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestValidationBeforeTimeCheck(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 8, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	// Both invalid input and backwards time: structural error wins.
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "BAD", 9}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "ok", 9}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestExpiryOnApplyAndClosedBoundary(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); err != nil {
		t.Fatal(err)
	}
	// Advancing to Now=5 evicts "a" (ExpiresAt <= Now), freeing capacity for "c".
	r, err := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 2 {
		t.Fatal(r.Generation)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatal(s.Entries)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// Batch evicts nothing, deletes "a", then puts two entries: over capacity.
	_, err := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}, {Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("state changed: %+v -> %+v", before, after)
	}
}

func TestRollbackOnNotFound(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// "a" is evicted by the candidate sweep (ExpiresAt 2 <= Now 2), so Touch fails.
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != 1 || after.Generation != before.Generation || len(after.Entries) != 1 {
		t.Fatalf("state changed: %+v", after)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 8, MaxKeyBytes: 8})
	r, err := x.Apply(Batch{Now: 0})
	if err != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, err)
	}
	if x.Snapshot().Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
	r, err = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if err != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, err)
	}
	s := x.Snapshot()
	if s.NextRevision != 3 || s.Entries[0].Revision != 1 || s.Entries[1].Revision != 2 {
		t.Fatal(s)
	}
}

func TestExpireMonotonicAndIsolation(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 8, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 4}, {Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Expire(2); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	gone, err := x.Expire(4)
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(gone, err)
	}
	gone[0].Key = "mutated"
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" || s.Now != 4 {
		t.Fatal(s)
	}
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "b" {
		t.Fatal("snapshot shares internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 128, MaxKeyBytes: 8})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("k%02d", i)
			for n := 0; n < 50; n++ {
				now := int64(n + 1)
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 100}}})
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Touch, k, now + 200}}})
				_, _ = x.Expire(now)
				_ = x.Snapshot()
			}
			_, _ = x.Apply(Batch{Now: 1000, Ops: []Op{{Delete, k, 0}}})
		}()
	}
	wg.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal("capacity violated")
	}
	for _, e := range s.Entries {
		if e.ExpiresAt <= s.Now {
			t.Fatalf("expired entry survived: %+v now=%d", e, s.Now)
		}
	}
}
