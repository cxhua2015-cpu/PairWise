package resourcelease104

import (
	"errors"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey", "汉"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 9}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}, {Kind(9), "b", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(3); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestCandidateEvictionClosedBound(t *testing.T) {
	x := table(t)
	// "old" expires at 2; batch at Now=2 must evict it before capacity check.
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "old", 2}, {Put, "x", 100}, {Put, "y", 100}}})
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "z", 100}}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Generation != 2 {
		t.Fatal(r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 3 || s.Entries[0].Key != "x" {
		t.Fatal(s.Entries)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}})
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Failed batch must not advance time, evict, or burn revisions.
	if got := x.Snapshot(); got.Now != b.Now || got.NextRevision != b.NextRevision || got.Generation != b.Generation || len(got.Entries) != 1 {
		t.Fatal(got)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	got := x.Snapshot()
	if got.Now != b.Now || got.NextRevision != b.NextRevision || len(got.Entries) != 2 {
		t.Fatal(got)
	}
}

func TestEmptyBatchNoop(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}}})
	b := x.Snapshot()
	r, e := x.Apply(Batch{Now: 5})
	if e != nil || r.Generation != b.Generation {
		t.Fatal(r, e)
	}
	got := x.Snapshot()
	if got.Generation != b.Generation || got.Now != b.Now || len(got.Entries) != 1 {
		t.Fatal(got)
	}
}

func TestTouchAndDeleteSemantics(t *testing.T) {
	x := table(t)
	r, _ := x.Apply(Batch{Ops: []Op{{Put, "a", 5}, {Touch, "a", 7}, {Delete, "a", 0}}})
	if r.Revision != 2 || len(x.Snapshot().Entries) != 0 {
		t.Fatal(r)
	}
	// Put overwrites and allocates a new revision.
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 5}}})
	r, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 6}}})
	if r.Revision != 4 || x.Snapshot().Entries[0].ExpiresAt != 6 {
		t.Fatal(r)
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
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, 100}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if s.Now != 20 || len(s.Entries) > 128 {
		t.Fatal(s.Now, len(s.Entries))
	}
}
