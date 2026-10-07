package balanceledger302

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{MaxAccounts: 0, MaxNameBytes: 1, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 0, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 1, MaxAbsValue: 0},
		{MaxAccounts: -1, MaxNameBytes: 1, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 1, MaxAbsValue: -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 16, MaxNameBytes: 8, MaxAbsValue: 100})
	bad := []string{"", "A", "a b", "a/b", "中文名", "toolongname", "a.b"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Add, n, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	good := []string{"a", "0", "-", "_", "a-b_c9", "12345678"}
	for _, n := range good {
		if _, e := l.Apply(Batch{Ops: []Op{{Add, n, 1, 0}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndFullValidation(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation happens before state reads: the unknown kind in
	// the second op must win over the ErrNotFound the first op would raise.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}, {Kind: 7, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -2, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", -40, 0}}}); e != nil {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != -20 {
		t.Fatal(got)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 2 {
		t.Fatal(n)
	}
	// Delete-then-create stays within capacity at batch end.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "c", 0, 3}}}); e != nil {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	s0 := l.Snapshot()
	if s0.Generation != 0 || s0.NextRevision != 1 {
		t.Fatal(s0)
	}
	// Empty batch: no generation bump.
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || l.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Delete, "a", 0, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	// Failed batch: no generation or revision change.
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 1}, {Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 2 {
		t.Fatal(s)
	}
}

func TestChangedContents(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 5}, {Add, "a", 2, 0}}})
	if e != nil || len(r.Changed) != 2 {
		t.Fatal(e, r)
	}
	if r.Changed[0].Name != "a" || r.Changed[0].Value != 3 || r.Changed[0].Revision != 3 {
		t.Fatal(r.Changed[0])
	}
	if r.Changed[1].Name != "b" || r.Changed[1].Revision != 2 {
		t.Fatal(r.Changed[1])
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -1}, {Set, "d", 0, 9},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(e, top)
	}
	want := []string{"d", "a", "b"}
	for i, name := range want {
		if top[i].Name != name {
			t.Fatal(top)
		}
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(all)
	}
	zero, _ := l.Top(0)
	if len(zero) != 0 {
		t.Fatal(zero)
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}})
	s := l.Snapshot()
	if s.Accounts[0].Name != "a" || s.Accounts[1].Name != "b" {
		t.Fatal(s)
	}
	s.Accounts[0].Value = 99
	top, _ := l.Top(2)
	top[0].Value = 99
	again := l.Snapshot()
	if again.Accounts[0].Value != 1 {
		t.Fatal(again)
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
				if _, e := l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}}); e != nil {
					t.Error(e)
					return
				}
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
	for _, acc := range s.Accounts {
		if acc.Value != 50 {
			t.Fatal(acc)
		}
	}
	if s.Generation != 16*50 || s.NextRevision != 16*50+1 {
		t.Fatal(s)
	}
}
