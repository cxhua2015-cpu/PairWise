package resourcelease199

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x, _ := New(Options{MaxEntries: 16, MaxKeyBytes: 8})
	bad := []string{"", "A", "a b", "a/b", "toolongkey", "中文", "a.b"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 9}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	for _, k := range []string{"a", "0", "-", "_", "abcd-12_"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 9}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}, {Put, "b", 2}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
}

func TestNegativeNow(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonic(t *testing.T) {
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
	if x.Snapshot().Now != 5 {
		t.Fatal(x.Snapshot().Now)
	}
}

func TestEmptyBatchNoop(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if s.Generation != 0 || s.Now != 0 || len(s.Entries) != 0 {
		t.Fatal(s)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 2}, {Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatal(s)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestExpiryRollbackOnCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	// "old" would be expired by Now=5, but the batch must roll back entirely.
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "old", 3}}}); e != nil {
		t.Fatal(e)
	}
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "n1", 9}, {Put, "n2", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "old" || s.Now != 0 || s.Generation != 1 || s.NextRevision != 2 {
		t.Fatal(s)
	}
}

func TestCandidateExpiryClosedBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}}); e != nil {
		t.Fatal(e)
	}
	// ExpiresAt <= Now removed before ops run; "a" (3) gone, "b" (4) stays.
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "c", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatal(s.Entries)
	}
}

func TestExpireClosedBoundaryAndResult(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}})
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" || gone[0].Revision != 1 {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r1, _ := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := x.Apply(Batch{Ops: []Op{{Touch, "a", 10}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	s := x.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 999
	if x.Snapshot().Entries[0].ExpiresAt != 9 {
		t.Fatal("snapshot aliases internal state")
	}
	gone, _ := x.Expire(9)
	gone[0].Key = "mutated"
	if _, e := x.Apply(Batch{Now: 9, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("returned slice aliases internal state")
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
				_ = x.Snapshot()
				_, _ = x.Expire(n)
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	// Concurrent goroutines share one monotonic clock, so some batches
	// legitimately fail with ErrTime; the race detector does the real work.
	if len(s.Entries) > 32 || s.Generation == 0 || s.Generation > 32*40 {
		t.Fatal(len(s.Entries), s.Generation)
	}
}
