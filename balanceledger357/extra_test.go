package balanceledger357

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
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	l := led(t)
	bad := []Op{
		{Kind(0), "a", 1, 0},
		{Kind(9), "a", 1, 0},
		{Add, "", 1, 0},
		{Add, "Upper", 1, 0},
		{Add, "has space", 1, 0},
		{Add, "toolongname", 1, 0},
		{Add, "中文", 1, 0},
	}
	for _, op := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestValidNameChars(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a-0_z", 0, 1}}}); e != nil {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "c", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 1 {
		t.Fatal(n)
	}
}

func TestAbsLimit(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t) // MaxAccounts 4
	ops := []Op{{Set, "a", 0, 1}, {Set, "b", 0, 1}, {Set, "c", 0, 1}, {Set, "d", 0, 1}}
	if _, e := l.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	b := l.Snapshot()
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "e", 0, 1}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if g := l.Snapshot().Generation; g != b.Generation {
		t.Fatal(g)
	}
	// Delete-then-create stays within final capacity.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "e", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionSequence(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if e != nil || x.Revision != 2 {
		t.Fatal(e, x)
	}
	s := l.Snapshot()
	if s.NextRevision != 3 || s.Accounts[0].Revision != 2 {
		t.Fatal(s)
	}
	// Failed batch must not consume revisions.
	_, _ = l.Apply(Batch{Ops: []Op{{Add, "b", 100, 0}}})
	x, _ = l.Apply(Batch{Ops: []Op{{Add, "b", 1, 0}}})
	if x.Revision != 3 {
		t.Fatal(x)
	}
}

func TestEmptyBatch(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{})
	if e != nil || x.Generation != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if l.Snapshot().Generation != 0 {
		t.Fatal("generation changed")
	}
}

func TestTopOrder(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(e, top)
	}
	want := []string{"c", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(all)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}, {Set, "a", 0, 1}}})
	s := l.Snapshot()
	if s.Accounts[0].Name != "a" || s.Accounts[1].Name != "b" {
		t.Fatal(s)
	}
	s.Accounts[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("internal state mutated")
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
				_, _ = l.Apply(Batch{Ops: []Op{{Set, k, 0, int64(j % 50)}}})
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
	prev := uint64(0)
	for _, a := range s.Accounts {
		if a.Revision == 0 || a.Revision >= s.NextRevision {
			t.Fatal(a)
		}
		_ = prev
	}
}

func TestChangedDedup(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "a", 1, 0}, {Set, "a", 0, 7}}})
	if e != nil || len(x.Changed) != 1 || x.Changed[0].Value != 7 || x.Changed[0].Revision != 3 {
		t.Fatal(e, x)
	}
	// Delete removes the account from Changed.
	x, e = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 1}, {Delete, "b", 0, 0}}})
	if e != nil || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
}
