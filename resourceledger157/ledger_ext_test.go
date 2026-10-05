package resourceledger157

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	l := led(t)
	bad := []Op{
		{Kind: 0, Name: "a"},
		{Kind: 99, Name: "a"},
		{Kind: Add, Name: ""},
		{Kind: Add, Name: "A"},
		{Kind: Add, Name: "a b"},
		{Kind: Add, Name: "a/b"},
		{Kind: Add, Name: "toolongname"}, // > 8 bytes
		{Kind: Add, Name: "a", Value: 1}, // extra field
		{Kind: Set, Name: "a", Delta: 1},
		{Kind: Delete, Name: "a", Delta: 1},
		{Kind: Delete, Name: "a", Value: 1},
	}
	for _, op := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	// Structural validation happens before state reads: unknown kind must
	// win over a missing account later in the batch.
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}, {Delete, "zz", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Valid name characters.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a-z_09", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed batches roll back; value unchanged.
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatal(got)
	}

	s, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := s.Apply(Batch{Ops: []Op{{Set, "a", 0, 11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Set, "a", 0, -10}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	l := led(t)
	r0, e := l.Apply(Batch{})
	if e != nil || r0.Generation != 0 || r0.Revision != 0 {
		t.Fatal(r0, e)
	}
	x, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}}})
	if e != nil || x.Generation != 1 || x.Revision != 2 {
		t.Fatal(x, e)
	}
	if len(x.Changed) != 1 || x.Changed[0].Name != "b" || x.Changed[0].Revision != 2 {
		t.Fatal(x.Changed)
	}
	snap := l.Snapshot()
	if snap.Generation != 1 || snap.NextRevision != 3 || len(snap.Accounts) != 1 {
		t.Fatal(snap)
	}
	// Failed batch must not consume generation or revisions.
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	y, e := l.Apply(Batch{Ops: []Op{{Add, "b", 1, 0}}})
	if e != nil || y.Generation != 2 || y.Revision != 3 {
		t.Fatal(y, e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
	// Delete-then-create within one batch stays within capacity.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "c", 0, 3}}}); e != nil {
		t.Fatal(e)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -1}, {Set, "d", 0, 9}}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "d" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatal(all)
	}
	if none, _ := l.Top(0); len(none) != 0 {
		t.Fatal(none)
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Snapshot sorted by name.
	names := ""
	for _, a := range l.Snapshot().Accounts {
		names += a.Name
	}
	if names != "abcd" {
		t.Fatal(names)
	}
	// Returned slices are isolated from internal state.
	top[0].Value = -99
	if again, _ := l.Top(1); again[0].Value != 9 {
		t.Fatal(again)
	}
}

func TestConcurrentStress(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				if _, e := l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}}); e != nil {
					t.Error(e)
					return
				}
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := l.Snapshot()
	if len(snap.Accounts) != 16 {
		t.Fatal(len(snap.Accounts))
	}
	for _, a := range snap.Accounts {
		if a.Value != 50 {
			t.Fatal(a)
		}
	}
	if snap.Generation != 16*50 || snap.NextRevision != 16*50+1 {
		t.Fatal(snap)
	}
}
