package balanceledger262

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
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestNameRules(t *testing.T) {
	l := led(t)
	for _, n := range []string{"", "A", "a b", "a.b", "toolongname", "é"} {
		if e := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-0_9", "abcdefgh"} {
		if e := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("%q: %v", n, e)
		}
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Add, "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
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
	l2 := led(t) // MaxAbsValue 20
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}}}); e != nil {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	if s := l.Snapshot(); s.Generation != 0 || s.NextRevision != 1 {
		t.Fatal(s)
	}
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || l.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 || len(r.Changed) != 1 {
		t.Fatal(e, r)
	}
	if s := l.Snapshot(); s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestTopOrderAndIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -1}}})
	top, e := l.Top(10)
	if e != nil || len(top) != 3 || top[0].Name != "a" || top[1].Name != "b" || top[2].Name != "c" {
		t.Fatal(e, top)
	}
	top[0].Value = 999
	if l.Snapshot().Accounts[0].Value == 999 {
		t.Fatal("returned slice aliases state")
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if got, e := l.Top(0); e != nil || len(got) != 0 {
		t.Fatal(e, got)
	}
	snap := l.Snapshot()
	if snap.Accounts[0].Name != "a" || snap.Accounts[2].Name != "c" {
		t.Fatal(snap)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	_, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}, {Set, "c", 0, 3}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 1 {
		t.Fatal(n)
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 3}}})
	c, e := l.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clocks not preserved")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 0 {
		t.Fatal("clone shares state")
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
				_ = l.Stats()
				_, _ = l.Top(4)
				_ = l.Snapshot()
				if j%10 == 0 {
					_, _ = l.Clone()
				}
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
			}
		}()
	}
	w.Wait()
	z := l.Stats()
	if z.Accounts != 32 || z.Generation != 32*50 {
		t.Fatalf("%+v", z)
	}
}
