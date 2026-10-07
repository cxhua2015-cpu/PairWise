package expirytable349

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "toolongkey", "中文", "a/b"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(5); e != nil {
		t.Fatal(e)
	}
}

func TestCandidateExpiryOnApply(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 10}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=3 (closed boundary) before ops run.
	r, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "c", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	var keys []string
	for _, en := range x.Snapshot().Entries {
		keys = append(keys, en.Key)
	}
	if !reflect.DeepEqual(keys, []string{"b", "c"}) {
		t.Fatal(keys)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 100}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Would evict "a" (ExpiresAt=2 <= Now=2) then add c,d -> 3 > 2: full rollback.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
}

func TestRollbackOnNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 50}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Touch, "missing", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 0})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}, {Touch, "a", 10}}})
	if e != nil || r.Generation != 1 || r.Revision != 3 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if s.NextRevision != 4 || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if len(x.Snapshot().Entries) != 1 {
		t.Fatal("expected one entry left")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	s.Entries[0].ExpiresAt = 0
	if x.Snapshot().Entries[0].Key != "a" {
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
			for n := int64(0); n < 20; n++ {
				now := n * 100
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 500}}})
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Touch, k, now + 900}}})
				_, _ = x.Expire(now)
				s := x.Snapshot()
				for _, e := range s.Entries {
					if e.ExpiresAt <= s.Now {
						panic("expired entry visible in snapshot")
					}
				}
			}
			_, _ = x.Expire(100000)
		}()
	}
	w.Wait()
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
}
