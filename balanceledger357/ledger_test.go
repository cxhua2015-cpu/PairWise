package balanceledger357

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func opts() Options { return Options{MaxAccounts: 8, MaxNameBytes: 4, MaxAbsValue: 100} }

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 4, 100}, {8, 0, 100}, {8, 4, 0}, {-1, 4, 100}, {8, -1, 100}, {8, 4, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l, _ := New(opts())
	bad := []string{"", "ABC", "a b", "a.b", "toolong", "é", "A"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	good := []string{"a", "z0-_", "0", "----", "____"}
	for _, n := range good {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l, _ := New(opts())
	for _, k := range []Kind{0, 4, 255} {
		if _, e := l.Apply(Batch{Ops: []Op{{k, "a", 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: %v", k, e)
		}
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	l, _ := New(opts())
	// Second op is structurally invalid: nothing may be applied, no revision burned.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Kind(9), "b", 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatal(s)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "c", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// State untouched after failures.
	if got := l.Snapshot().Accounts; len(got) != 2 || got[0].Value != math.MaxInt64 {
		t.Fatal(got)
	}
}

func TestAbsLimitBoundary(t *testing.T) {
	l, _ := New(opts()) // MaxAbsValue 100
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 100}, {Set, "b", 0, -100}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 101}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestRevisionSequenceAndGeneration(t *testing.T) {
	l, _ := New(opts())
	r1, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Add, "b", 2, 0}}})
	if e != nil || r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(e, r1)
	}
	// Delete consumes no revision: b got revision 2.
	if r1.Changed[0].Name != "b" || r1.Changed[0].Revision != 2 {
		t.Fatal(r1.Changed)
	}
	// Empty batch: generation unchanged.
	r2, e := l.Apply(Batch{})
	if e != nil || r2.Generation != 1 || len(r2.Changed) != 0 {
		t.Fatal(e, r2)
	}
	// Failed batch: no generation bump, no revision burn.
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "c", 0, 1}, {Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r3, e := l.Apply(Batch{Ops: []Op{{Add, "b", 1, 0}}})
	if e != nil || r3.Generation != 2 || r3.Revision != 3 {
		t.Fatal(e, r3)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	// Net +1 account exceeds capacity only at batch end.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Set, "d", 0, 4}, {Delete, "c", 0, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 2 {
		t.Fatal(n)
	}
	// Peak over capacity but final within: allowed.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Delete, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteNotFound(t *testing.T) {
	l, _ := New(opts())
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "g", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(opts())
	_, e := l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	if e != nil {
		t.Fatal(e)
	}
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(e, top)
	}
	want := []string{"c", "a", "b"} // value desc, name asc on ties
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "d" {
		t.Fatal(all)
	}
	zero, e := l.Top(0)
	if e != nil || len(zero) != 0 {
		t.Fatal(e, zero)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l, _ := New(opts())
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	top, _ := l.Top(1)
	r.Changed[0].Value = 999
	s.Accounts[0].Value = 999
	top[0].Value = 999
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatal(got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 100, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			name := string(rune('a'+i%26)) + string(rune('a'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Set, name, 0, int64(j)}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 32 {
		t.Fatal(len(s.Accounts))
	}
	// Revisions are unique and dense up to NextRevision-1.
	seen := make(map[uint64]bool)
	for _, a := range s.Accounts {
		if a.Revision == 0 || a.Revision >= s.NextRevision || seen[a.Revision] {
			t.Fatal(a, s.NextRevision)
		}
		seen[a.Revision] = true
	}
}

func TestConcurrentSameAccount(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	const g, n = 16, 100
	for i := 0; i < g; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < n; j++ {
				if _, e := l.Apply(Batch{Ops: []Op{{Add, "x", 1, 0}}}); e != nil {
					t.Error(e)
				}
			}
		}()
	}
	w.Wait()
	if got := l.Snapshot().Accounts[0].Value; got != g*n {
		t.Fatal(got)
	}
}
