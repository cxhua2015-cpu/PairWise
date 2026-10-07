package balanceledger312

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {1, 1, -5}, {-1, 8, 20},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, n := range []string{"", "A", "a b", "a/b", "中文", "toolongname", "a.b", "+x"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z0-_", "12345678"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(0), "a", 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(4), "a", 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	s := l.Snapshot()
	if s.Generation != 0 || s.NextRevision != 1 {
		t.Fatal(s)
	}
}

func TestOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	// MaxInt64 + 1 overflows; must be detected before arithmetic.
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// MinInt64 delta is rejected as input too.
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed batches roll back: a still MaxInt64.
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatal(got)
	}
}

func TestAbsLimit(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}}}); e != nil {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t) // MaxAccounts 4
	ops := []Op{{Set, "a", 0, 1}, {Set, "b", 0, 1}, {Set, "c", 0, 1}, {Set, "d", 0, 1}}
	if _, e := l.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	before := l.Snapshot()
	// Add a 5th account mid-batch then fail on value: full rollback.
	_, e := l.Apply(Batch{Ops: []Op{{Set, "e", 0, 1}, {Set, "a", 0, 99}}})
	if !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot(); got.Generation != before.Generation || len(got.Accounts) != 4 {
		t.Fatal(got)
	}
	// Pure capacity overflow.
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "e", 0, 1}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestRevisionSequence(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Set, "c", 0, 3}}})
	if e != nil || r.Revision != 3 || r.Generation != 1 {
		t.Fatal(r, e)
	}
	// Changed holds final state of mutated accounts in first-touch order.
	if len(r.Changed) != 3 || r.Changed[0].Name != "a" || r.Changed[1].Name != "b" || r.Changed[2].Name != "c" {
		t.Fatal(r.Changed)
	}
	if r.Changed[1].Revision != 2 || r.Changed[2].Revision != 3 {
		t.Fatal(r.Changed)
	}
	s := l.Snapshot()
	if s.NextRevision != 4 || len(s.Accounts) != 2 || s.Accounts[0].Name != "b" || s.Accounts[1].Name != "c" {
		t.Fatal(s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1}}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	want := []string{"c", "a", "b"} // value desc, name asc on ties
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatal(all)
	}
	if none, _ := l.Top(0); len(none) != 0 {
		t.Fatal(none)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 77
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
	// 16 goroutines * 50 successful single-op batches.
	if s.Generation != 800 || s.NextRevision != 801 {
		t.Fatal(s)
	}
}
