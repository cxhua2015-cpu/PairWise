package tokenvault

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestInvalidKeysAndKind(t *testing.T) {
	x := table(t)
	bad := []Op{
		{Put, "", 1}, {Put, "A", 1}, {Put, "a b", 1}, {Put, "a.b", 1},
		{Put, "toolongkey", 1}, {Put, "中文", 1}, {Kind(0), "a", 1}, {Kind(9), "a", 1},
	}
	for _, op := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	for _, k := range []string{"a", "z-0_", "abc-123"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 9}}}); e != nil {
			t.Fatalf("%q: %v", k, e)
		}
	}
}

func TestMonotonicTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Now != 2 {
		t.Fatal("now not advanced")
	}
	r, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Now: 4})
	if r.Generation != 1 || x.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	r1, _ := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 50}, {Put, "b", 50}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 50}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Generation != before.Generation || after.NextRevision != before.NextRevision ||
		after.Now != before.Now || len(after.Entries) != 2 {
		t.Fatalf("state changed: %+v -> %+v", before, after)
	}
	// Sweep frees capacity: entries expiring at Now are removed before capacity check.
	r2, e := x.Apply(Batch{Now: 60, Ops: []Op{{Put, "c", 99}}})
	if e != nil || len(x.Snapshot().Entries) != 1 {
		t.Fatal(e)
	}
	if r2.Revision <= r1.Revision {
		t.Fatal("revision not increasing")
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "missing", 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Error mid-batch rolls back earlier ops and revisions too.
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "b", 9}, {Touch, "missing", 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Generation != before.Generation || after.NextRevision != before.NextRevision ||
		len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("state changed: %+v -> %+v", before, after)
	}
}

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}, {Put, "c", 5}}})
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
	gone, _ = x.Expire(4)
	if len(gone) != 0 {
		t.Fatal(gone)
	}
}

func TestSweepOnApply(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}}})
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal("swept entry must not be touchable:", e)
	}
	// Failed batch rolls back the sweep too: "a" survives.
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	for _, e := range x.Snapshot().Entries {
		if e.Key == "a" {
			t.Fatal("sweep did not run on successful batch")
		}
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "hacked"
	s.Entries[0].ExpiresAt = 0
	e := x.Snapshot().Entries[0]
	if e.Key != "a" || e.ExpiresAt != 9 {
		t.Fatal("snapshot shares state")
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
			k := fmt.Sprintf("k%02d", i)
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_, _ = x.Expire(n)
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
			t.Fatalf("expired entry survived: %+v now=%d", e, s.Now)
		}
	}
}
