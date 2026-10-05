package resourceledger112

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {4, 8, -1}, {-1, 8, 10},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, n := range []string{"", "A", "a b", "a/b", "中文", "toolongname", "a.b"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "0", "-", "_", "a-b_c9", "12345678"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Delete, n, 0, 0}}}); !errors.Is(e, ErrNotFound) {
			t.Fatalf("%q: %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	for _, k := range []Kind{0, 4, 255} {
		if _, e := l.Apply(Batch{Ops: []Op{{Kind: k, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(k, e)
		}
	}
	// Structural validation happens before state reads: unknown kind wins
	// even when an earlier op would fail against state.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}, {Kind: 9, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// State untouched after overflow failures.
	if got := l.Snapshot().Accounts[0].Value; got != math.MinInt64+1 {
		t.Fatal(got)
	}
}

func TestAbsLimit(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -40, 0}}}); !errors.Is(e, ErrValue) {
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
	// Intermediate state fits, final state exceeds -> rollback everything.
	_, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "e", 0, 1}, {Set, "f", 0, 1}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 4 || s.Accounts[0].Name != "a" {
		t.Fatal(s)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	if s := l.Snapshot(); s.Generation != 0 || s.NextRevision != 1 {
		t.Fatal(s)
	}
	// Empty batch: no generation bump.
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	// Failed batch: no generation bump, no revision consumed.
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 99}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if s := l.Snapshot(); s.Generation != 0 || s.NextRevision != 1 {
		t.Fatal(s)
	}
	r, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	// Delete allocates no revision; Changed reflects final surviving account.
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Changed[0].Revision != 2 {
		t.Fatal(r.Changed)
	}
	if s := l.Snapshot(); s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "c", 0, 5}, {Set, "a", 0, 5}, {Set, "b", 0, -3}, {Set, "d", 0, 9},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	if top[0].Name != "d" || top[1].Name != "a" || top[2].Name != "c" {
		t.Fatal(top)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if top, _ = l.Top(0); len(top) != 0 {
		t.Fatal(top)
	}
	if top, _ = l.Top(100); len(top) != 4 {
		t.Fatal(top)
	}
}

func TestIsolation(t *testing.T) {
	l := led(t)
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r.Changed[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 99
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatal(got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}, {Add, k, -1, 0}}})
				_, _ = l.Top(4)
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
		if a.Value != 0 {
			t.Fatal(a)
		}
	}
	// Revisions are consecutive: 16 accounts * 100 Add ops.
	if s.NextRevision != 1601 {
		t.Fatal(s.NextRevision)
	}
	if s.Generation != 800 {
		t.Fatal(s.Generation)
	}
}
