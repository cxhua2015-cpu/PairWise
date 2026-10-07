package balanceledger352

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
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	l := led(t)
	for _, b := range []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Add, "", 1, 0}}},
		{Ops: []Op{{Add, "Upper", 1, 0}}},
		{Ops: []Op{{Add, "has space", 1, 0}}},
		{Ops: []Op{{Add, "toolongname", 1, 0}}},
		{Ops: []Op{{Add, "a/b", 1, 0}}},
	} {
		if _, e := l.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	// Structural validation happens before state reads: unknown kind wins
	// even when an earlier op would fail against state.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}, {Kind: 7, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// State untouched after failures.
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != math.MaxInt64 {
		t.Fatal(s)
	}
}

func TestAbsLimitEnforced(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", -40, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t) // MaxAccounts 4
	ops := []Op{}
	for _, n := range []string{"a", "b", "c", "d"} {
		ops = append(ops, Op{Set, n, 0, 1})
	}
	if _, e := l.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	// Transiently over capacity is fine, but final count must fit.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "e", 0, 1}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "e", 0, 1}, {Delete, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 4 {
		t.Fatal(n)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r0, e := l.Apply(Batch{})
	if e != nil || r0.Generation != 0 || l.Snapshot().Generation != 0 {
		t.Fatal(r0, e)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	// Failed batch does not bump generation or consume revisions.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	r2, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	if r2.Generation != 2 || r2.Revision != 3 || len(r2.Changed) != 1 || r2.Changed[0].Revision != 3 {
		t.Fatal(r2)
	}
}

func TestChangedDeduplicates(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "a", 1, 0}, {Set, "a", 0, 9}}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Value != 9 || r.Changed[0].Revision != 3 {
		t.Fatal(r, e)
	}
	// Delete removes the account from Changed.
	r, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 5}, {Delete, "a", 0, 0}}})
	if e != nil || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	want := []string{"c", "a", "b"}
	for i, n := range want {
		if top[i].Name != n {
			t.Fatal(top)
		}
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "d" {
		t.Fatal(all)
	}
	empty, _ := l.Top(0)
	if len(empty) != 0 {
		t.Fatal(empty)
	}
	// Returned slice is isolated from internal state.
	top[0].Value = -99
	again, _ := l.Top(1)
	if again[0].Value != 9 {
		t.Fatal(again)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "z", 0, 1}, {Set, "m", 0, 2}, {Set, "a", 0, 3}}})
	s := l.Snapshot()
	if len(s.Accounts) != 3 || s.Accounts[0].Name != "a" || s.Accounts[1].Name != "m" || s.Accounts[2].Name != "z" {
		t.Fatal(s)
	}
	s.Accounts[0].Value = -100
	if l.Snapshot().Accounts[0].Value != 3 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(8)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 16 {
		t.Fatal(len(s.Accounts))
	}
	for _, a := range s.Accounts {
		if a.Value != 50 {
			t.Fatal(a)
		}
	}
	// Revisions are unique and contiguous.
	seen := map[uint64]bool{}
	for _, a := range s.Accounts {
		if seen[a.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[a.Revision] = true
	}
	if s.NextRevision != 801 {
		t.Fatal(s.NextRevision)
	}
}
