package resourcelease189

import (
	"errors"
	"fmt"
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

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a/b"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 0, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 99, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNegativeNow(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 7}, {Put, "b", 8}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(7)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestRollbackOnNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	// Touch of a missing key after a valid Put must roll back the Put,
	// the expiration of "a" (ExpiresAt=2 <= Now=5), time and revision.
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}, {Kind: Touch, Key: "ghost", ExpiresAt: 9}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal(e, x.Snapshot())
	}
	_, e = x.Apply(Batch{Now: 5, Ops: []Op{{Kind: Delete, Key: "ghost"}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal(e, x.Snapshot())
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "b", ExpiresAt: 9}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal(e, x.Snapshot())
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}, {Touch, "a", 10}}})
	if e != nil || r.Generation != 1 || r.Revision != 3 {
		t.Fatal(e, r)
	}
	// Empty batch: generation unchanged.
	r, e = x.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 1 || x.Snapshot().Generation != 1 {
		t.Fatal(e, r)
	}
	// Failed batch: generation and revision unchanged.
	if _, e = x.Apply(Batch{Now: 1, Ops: []Op{{Kind: Touch, Key: "zz", ExpiresAt: 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Generation != 1 || s.NextRevision != 4 {
		t.Fatal(s)
	}
	r, e = x.Apply(Batch{Now: 1, Ops: []Op{{Kind: Delete, Key: "b"}}})
	if e != nil || r.Generation != 2 || r.Revision != 3 {
		t.Fatal(e, r)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mut"
	s.Entries[0].ExpiresAt = 0
	again := x.Snapshot()
	if again.Entries[0].Key != "a" || again.Entries[0].ExpiresAt != 9 {
		t.Fatal(again)
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
			for j := 1; j <= 50; j++ {
				_, _ = x.Apply(Batch{Now: int64(j), Ops: []Op{{Put, k, int64(j + 10)}}})
				_, _ = x.Apply(Batch{Now: int64(j), Ops: []Op{{Touch, k, int64(j + 20)}}})
				_, _ = x.Expire(int64(j))
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	if _, e := x.Expire(1000); e != nil {
		t.Fatal(e)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
}
