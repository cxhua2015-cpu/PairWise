package expirytable384

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "toolongkey", "汉"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 0, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExpiryBeforeOps(t *testing.T) {
	x := table(t)
	// Entry expires at exactly Now (closed interval) before Touch runs.
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}}})
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "a", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Put after expiry reuses the freed slot.
	r, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" || s.Now != 3 {
		t.Fatal(s)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	b := x.Snapshot()
	// Expiry frees "a", but two puts still overflow: full rollback expected.
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != b.Now || got.Generation != b.Generation ||
		got.NextRevision != b.NextRevision || len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatalf("state changed: %+v", got)
	}
}

func TestRollbackNotFound(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}, {Delete, "missing", 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != b.Now || got.Generation != b.Generation ||
		got.NextRevision != b.NextRevision || len(got.Entries) != 1 {
		t.Fatalf("state changed: %+v", got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r0, e := x.Apply(Batch{})
	if e != nil || r0.Generation != 0 || r0.Revision != 0 {
		t.Fatal(e, r0)
	}
	r1, _ := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}, {Touch, "a", 10}}})
	if r1.Generation != 1 || r1.Revision != 3 {
		t.Fatal(r1)
	}
	// Failed batch does not consume generation or revisions.
	_, _ = x.Apply(Batch{Ops: []Op{{Touch, "nope", 1}}})
	r2, _ := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	s := x.Snapshot()
	if s.NextRevision != 4 || s.Generation != 2 {
		t.Fatal(s)
	}
}

func TestExpireBoundaryAndTime(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 4}, {Put, "b", 5}, {Put, "c", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "d", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, _ = x.Expire(5)
	if len(gone) != 0 {
		t.Fatal(gone)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 0
	if x.Snapshot().Entries[0].ExpiresAt != 9 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_, _ = x.Expire(n)
				s := x.Snapshot()
				for _, e := range s.Entries {
					if e.Revision == 0 || e.Revision >= s.NextRevision {
						t.Errorf("bad revision: %+v vs %+v", e, s)
					}
				}
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal(len(s.Entries))
	}
}
