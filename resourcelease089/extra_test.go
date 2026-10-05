package resourcelease089

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey"}
	for _, k := range bad {
		_, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "a", 3}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCandidateEvictionClosedBound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}}}); e != nil {
		t.Fatal(e)
	}
	// Now=2 evicts a (ExpiresAt<=Now) inside candidate, leaving room for c.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatal(s.Entries)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r1, _ := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 100}}})
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 6, Ops: []Op{{Put, "b", 1}, {Put, "c", 1}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != b.Now || got.Generation != b.Generation ||
		got.NextRevision != b.NextRevision || len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatal(got)
	}
	if r1.Generation != 1 || r1.Revision != 1 {
		t.Fatal(r1)
	}
}

func TestRollbackNotFound(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	b := x.Snapshot()
	// Touch of missing key after a valid Put must roll back the Put and eviction.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Touch, "zz", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	got := x.Snapshot()
	// a expired at Now=2 only inside the candidate; rollback restores it.
	if got.Now != b.Now || got.Generation != b.Generation || got.NextRevision != b.NextRevision ||
		len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatal(got)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 || x.Snapshot().Generation != 0 || x.Snapshot().Now != 3 {
		t.Fatal(r, e)
	}
	r, _ = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if r.Generation != 1 || x.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestExpireTimeAndIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 3}}})
	if _, e := x.Expire(1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mut"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Revision != 1 {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
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
			for now := int64(1); now <= 10; now++ {
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 5}}})
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Touch, k, now + 6}}})
				_, _ = x.Expire(now)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if s.Generation == 0 || s.NextRevision <= 1 {
		t.Fatal(s)
	}
	for _, e := range s.Entries {
		if e.ExpiresAt <= s.Now {
			t.Fatal("expired entry survived", e, s.Now)
		}
	}
}
