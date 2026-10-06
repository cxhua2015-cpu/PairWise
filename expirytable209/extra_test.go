package expirytable209

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {3, 0}, {-1, 8}, {3, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey", "é"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "abcd-123", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "123456789", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -2}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "nope", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "nope", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 7, Ops: []Op{{Put, "a", 9}, {Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != b.Now || got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Entries) != 0 {
		t.Fatalf("rollback failed: %+v", got)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 100, Ops: []Op{{Put, "b", 200}, {Put, "c", 200}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	got := x.Snapshot()
	if got.Now != b.Now || got.NextRevision != b.NextRevision || len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatalf("rollback failed: %+v", got)
	}
}

func TestExpiryOnApplyAndGeneration(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 5}}}); e != nil {
		t.Fatal(e)
	}
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatalf("%+v", s)
	}
	g := s.Generation
	r, e = x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != g || x.Snapshot().Generation != g {
		t.Fatal(e, r)
	}
	if x.Snapshot().Now != 3 {
		t.Fatal("empty batch should still advance time")
	}
}

func TestExpireBoundaryAndTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 4}, {Put, "b", 5}}}); e != nil {
		t.Fatal(e)
	}
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
	gone, e = x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "b" {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 0
	gone, _ := x.Expire(0)
	if len(gone) != 0 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%d", i)
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 16 {
		t.Fatal(len(s.Entries))
	}
	if s.NextRevision < 2 {
		t.Fatal(s.NextRevision)
	}
}
