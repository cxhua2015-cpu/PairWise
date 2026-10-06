package balanceledger252

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {1, 1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, n := range []string{"", "A", "a b", "a/b", "toolongname", "é"} {
		if e := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Set, "ok-n_1", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Kind(9), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
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
	l2, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "a", 11, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if e := l2.ValidateBatch(Batch{Ops: []Op{{Add, "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	g := l.Snapshot().Generation
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != g || len(s.Accounts) != 2 {
		t.Fatal("rollback failed", s)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	if g := l.Stats().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1}}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	top[0].Value = -999
	if l.Snapshot().Accounts[2].Value == -999 {
		t.Fatal("returned slice aliases state")
	}
}

func TestDeleteAndRevisionSequence(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Add, "b", 3, 0}}})
	if e != nil || r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if n := l.Stats().NextRevision; n != 3 {
		t.Fatal(n)
	}
}

func TestCloneIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 7}}})
	c, e := l.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clocks not preserved")
	}
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if v := c.Snapshot().Accounts[0].Value; v != 7 {
		t.Fatal("clone mutated by original", v)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
				_ = l.Stats()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
			}
		}()
	}
	w.Wait()
	s := l.Stats()
	if int(s.Accounts) != len(l.Snapshot().Accounts) {
		t.Fatal("stats inconsistent")
	}
	c, e := l.Clone()
	if e != nil || c.Stats() != s {
		t.Fatal(e, c.Stats(), s)
	}
}
