package expirytable214

import (
	"errors"
	"reflect"
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

func TestStructuralBeforeTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 5}}}); e != nil {
		t.Fatal(e)
	}
	// Now moves backwards AND the op is invalid: structural check wins.
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "UPPER", 5}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "toolongkey", "中文"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestClosedBoundaryEviction(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	// ExpiresAt <= Now is evicted inside Apply: "a" gone, "b" stays.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "b", 7}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s.Entries)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	// Evicts "a" (ExpiresAt 2 <= Now 2), then two puts overflow capacity 1.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(b, got) {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestRollbackNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "a", 1}, {Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(b, got) {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	r, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
	s := x.Snapshot()
	if s.NextRevision != 3 || s.Generation != 2 {
		t.Fatal(s)
	}
}

func TestExpireMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 4}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(3); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if x.Snapshot().Now != 4 {
		t.Fatal("now not advanced")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mut"
	s.Entries[0].ExpiresAt = 1
	again := x.Snapshot()
	if again.Entries[0].Key != "a" || again.Entries[0].ExpiresAt != 9 {
		t.Fatal("snapshot aliases internal state")
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
			k := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
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
