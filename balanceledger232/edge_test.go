package balanceledger232

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
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestValidation(t *testing.T) {
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
		{Batch{Ops: []Op{{Add, "ok-1", 21, 0}}}, ErrValue},
		{Batch{Ops: []Op{{Add, "ok-1", -21, 0}}}, ErrValue},
		{Batch{Ops: []Op{{Set, "a", 0, 21}}}, ErrValue},
		{Batch{Ops: []Op{{Set, "a", 0, -21}}}, ErrValue},
		{Batch{Ops: []Op{{Add, "ok-1", 20, 0}}}, nil},
		{Batch{Ops: []Op{{Set, "ok-1", 0, -20}}}, nil},
		{Batch{Ops: []Op{{Delete, "ok-1", 0, 0}}}, nil},
		{Batch{}, nil},
	}
	for i, c := range cases {
		if e := l.ValidateBatch(c.b); e != c.e {
			t.Fatalf("case %d: got %v want %v", i, e, c.e)
		}
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MinInt64+1 {
		t.Fatal(got)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}}})
	if r.Generation != 0 {
		t.Fatal(r)
	}
	r, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
	if s := l.Stats(); s.Generation != 1 || s.NextRevision != 2 || s.Accounts != 1 {
		t.Fatal(s)
	}
}

func TestTopOrderAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	top[0].Value = -999
	if l.Snapshot().Accounts[2].Name != "c" || l.Snapshot().Accounts[2].Value != 9 {
		t.Fatal("snapshot aliases Top result")
	}
	snap := l.Snapshot()
	names := []string{}
	for _, a := range snap.Accounts {
		names = append(names, a.Name)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatal(names)
		}
	}
	snap.Accounts[0].Value = 777
	if l.Snapshot().Accounts[0].Value == 777 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}})
	_, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Set, "d", 0, 4}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 2 {
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
		t.Fatal("clone lost logical clocks")
	}
	if _, e = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "z", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 1 {
		t.Fatal("clone shares state")
	}
	if l.Snapshot().Accounts[0].Name != "a" || c.Snapshot().Accounts[0].Name != "z" {
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
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := l.Stats()
	if s.Accounts != 16 || s.Generation != 800 || s.NextRevision != 801 {
		t.Fatal(s)
	}
	for _, a := range l.Snapshot().Accounts {
		if a.Value != 50 {
			t.Fatal(a)
		}
	}
}
