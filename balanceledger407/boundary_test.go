package balanceledger407

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

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"}
	for _, n := range bad {
		if e := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	good := []string{"a", "z0-_", "12345678"}
	for _, n := range good {
		if e := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("%q: %v", n, e)
		}
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestOverflowGuards(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e) // |MinInt64| exceeds MaxAbsValue
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -math.MaxInt64}, {Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Stats().Accounts; got != 1 {
		t.Fatal(got) // failed batches rolled back
	}
}

func TestAbsLimitAndRollback(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}, {Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if s := l.Snapshot(); len(s.Accounts) != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestCapacityOnlyAtBatchEnd(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	// Peak of 3 accounts mid-batch, final 2: must succeed.
	r, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}, {Delete, "c", 0, 0}}})
	if e != nil || len(r.Changed) != 3 {
		t.Fatal(e, r)
	}
	// Final 3 accounts: must fail with ErrCapacity and roll back fully.
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := l.Stats().Accounts; got != 2 {
		t.Fatal(got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	l := led(t)
	r1, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if e != nil || r1.Generation != 1 {
		t.Fatal(e, r1)
	}
	r2, e := l.Apply(Batch{})
	if e != nil || r2.Generation != 1 || r2.Revision != r1.Revision || r2.Changed != nil {
		t.Fatal(e, r2)
	}
}

func TestTopOrderAndLimit(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -1}}})
	top, e := l.Top(2)
	if e != nil || len(top) != 2 || top[0].Name != "a" || top[1].Name != "b" {
		t.Fatal(e, top)
	}
	all, _ := l.Top(99)
	if len(all) != 3 || all[2].Name != "c" {
		t.Fatal(all)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestReturnedSlicesAreIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	top, _ := l.Top(1)
	top[0].Value = 999
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatal(got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				if j%10 == 0 {
					c, err := l.Clone()
					if err == nil {
						_, _ = c.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
					}
				}
			}
		}()
	}
	w.Wait()
	z := l.Stats()
	if z.Accounts == 0 || z.NextRevision != uint64(32*50)+1 {
		t.Fatalf("%+v", z)
	}
	// Clone independence: original clocks unchanged by clone writes.
	c, _ := l.Clone()
	_, _ = c.Apply(Batch{Ops: []Op{{Set, "zz", 0, 1}}})
	if l.Stats().Generation != z.Generation {
		t.Fatal("clone write leaked into original")
	}
}
