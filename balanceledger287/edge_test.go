package balanceledger287

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestValidateStructural(t *testing.T) {
	l := led(t)
	cases := []struct {
		b Batch
		e error
	}{
		{Batch{Ops: []Op{{Kind: 0, Name: "a"}}}, ErrInvalidInput},
		{Batch{Ops: []Op{{Kind: 99, Name: "a"}}}, ErrInvalidInput},
		{Batch{Ops: []Op{{Add, "", 1, 0}}}, ErrInvalidInput},
		{Batch{Ops: []Op{{Add, "A", 1, 0}}}, ErrInvalidInput},
		{Batch{Ops: []Op{{Add, "a b", 1, 0}}}, ErrInvalidInput},
		{Batch{Ops: []Op{{Add, "toolongname", 1, 0}}}, ErrInvalidInput},
		{Batch{Ops: []Op{{Add, "a", 0, 0}}}, ErrInvalidInput},
		{Batch{Ops: []Op{{Add, "a", 1, 1}}}, ErrInvalidInput},
		{Batch{Ops: []Op{{Set, "a", 1, 0}}}, ErrInvalidInput},
		{Batch{Ops: []Op{{Delete, "a", 1, 0}}}, ErrInvalidInput},
		{Batch{Ops: []Op{{Delete, "a", 0, 1}}}, ErrInvalidInput},
		{Batch{Ops: []Op{{Set, "a", 0, 21}}}, ErrValue},
		{Batch{Ops: []Op{{Set, "a", 0, -21}}}, ErrValue},
		{Batch{Ops: []Op{{Add, "ok-nm_1", 1, 0}}}, nil},
		{Batch{Ops: []Op{{Set, "a", 0, 20}}}, nil},
		{Batch{Ops: []Op{{Delete, "missing", 0, 0}}}, nil},
		{Batch{}, nil},
	}
	for i, c := range cases {
		if e := l.ValidateBatch(c.b); !errors.Is(e, c.e) {
			t.Fatalf("case %d: got %v want %v", i, e, c.e)
		}
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal("ValidateBatch mutated state")
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("overflow up: %v", e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -2, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("overflow down: %v", e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MinInt64+1 {
		t.Fatalf("state mutated on failure: %d", got)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	g := l.Snapshot().Generation
	_, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Set, "d", 0, 4}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 2 || s.Generation != g {
		t.Fatalf("not rolled back: %+v", s)
	}
	// Intermediate overflow within capacity is fine; only final count matters.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 9}, {Delete, "c", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	r, _ = l.Apply(Batch{})
	if r.Generation != 1 || l.Stats().Generation != 1 {
		t.Fatalf("empty batch changed generation: %+v", r)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3}, {Set, "d", 0, 9},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	want := []string{"d", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatalf("top[%d]=%s want %s", i, top[i].Name, w)
		}
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(len(all))
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	top, _ := l.Top(1)
	top[0].Value = 999
	r, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	r.Changed[0].Value = 999
	if got := l.Snapshot().Accounts[0].Value; got != 2 {
		t.Fatalf("internal state aliased: %d", got)
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 3}, {Set, "b", 0, 4}}})
	c, e := l.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clocks not preserved")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if len(l.Snapshot().Accounts) != 2 || len(c.Snapshot().Accounts) != 1 {
		t.Fatal("clone shares state")
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
				_ = l.Stats()
				_ = l.Snapshot()
				_, _ = l.Top(4)
				if j%10 == 0 {
					c, err := l.Clone()
					if err == nil {
						_ = c.Stats()
					}
				}
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	if st.Accounts != 16 || st.Generation != 16*50 {
		t.Fatalf("%+v", st)
	}
	for _, a := range l.Snapshot().Accounts {
		if a.Value != 50 {
			t.Fatalf("%s=%d", a.Name, a.Value)
		}
	}
}
