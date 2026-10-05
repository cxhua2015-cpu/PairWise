package resourcelease189

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	x := table(t)
	bad := []Op{
		{Kind: 0, Key: "a", ExpiresAt: 1},
		{Kind: 99, Key: "a", ExpiresAt: 1},
		{Kind: Put, Key: "", ExpiresAt: 1},
		{Kind: Put, Key: "Upper", ExpiresAt: 1},
		{Kind: Put, Key: "bad key", ExpiresAt: 1},
		{Kind: Put, Key: "toolongkey", ExpiresAt: 1},
		{Kind: Put, Key: "a", ExpiresAt: -1},
	}
	for _, op := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Generation != 0 || len(s.Entries) != 0 {
		t.Fatalf("state changed: %+v", s)
	}
}

func TestTimeMonotonicAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 10}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("state changed after time error")
	}
}

func TestCandidateEvictionAndNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}}}); e != nil {
		t.Fatal(e)
	}
	// Entry "a" expires at 3 <= Now=3, so Touch must fail with ErrNotFound.
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Failed batch must roll back eviction, time and revision.
	s := x.Snapshot()
	if s.Now != 1 || len(s.Entries) != 1 || s.NextRevision != 2 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("capacity failure did not roll back")
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 || x.Snapshot().Now != 0 {
		t.Fatalf("empty batch changed state: %+v %v", r, e)
	}
	r, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestExpireBoundaryAndIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatalf("%v %v", gone, e)
	}
	gone[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "b" {
		t.Fatal("returned slice aliases internal state")
	}
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 0
	if x.Snapshot().Entries[0].ExpiresAt != 6 {
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
			k := fmt.Sprintf("k%02d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 100}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 32 || s.Generation == 0 {
		t.Fatalf("entries=%d generation=%d", len(s.Entries), s.Generation)
	}
	for i, e := range s.Entries {
		if i > 0 && s.Entries[i-1].Key >= e.Key {
			t.Fatal("snapshot not sorted")
		}
	}
}
