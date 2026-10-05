package budgetledger

import (
	"errors"
	"fmt"
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
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongname", "a/b"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: n, Value: 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
	good := []string{"a", "z-0_9", "12345678"}
	for _, n := range good {
		if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: n, Value: 1}}}); e != nil {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Kind(0), Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Kind(99), Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Add, Name: "a", Delta: 1}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "b", Value: -math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Add, Name: "b", Delta: -1}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Add, Name: "a", Delta: math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "c", Value: math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestAbsLimit(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 20}, {Kind: Add, Name: "a", Delta: -40}}}); e != nil {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != -20 {
		t.Fatal(got)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 1}, {Kind: Set, Name: "b", Value: 2}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "c", Value: 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 2 {
		t.Fatal(n)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
	r0, e := l.Apply(Batch{})
	if e != nil || r0.Generation != 0 {
		t.Fatal(e, r0)
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal("empty batch bumped generation")
	}
	r1, e := l.Apply(Batch{Ops: []Op{{Kind: Add, Name: "a", Delta: 1}, {Kind: Set, Name: "a", Value: 2}}})
	if e != nil || r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(e, r1)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Delete, Name: "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	if len(s.Accounts) != 1 || s.Accounts[0].Revision != 2 {
		t.Fatal(s.Accounts)
	}
}

func TestDeleteMissingAndReadd(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Delete, Name: "ghost"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 5}, {Kind: Delete, Name: "a"}, {Kind: Add, Name: "a", Delta: 1}}}); e != nil {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatal(got)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Kind: Set, Name: "b", Value: 5},
		{Kind: Set, Name: "a", Value: 5},
		{Kind: Set, Name: "c", Value: 9},
		{Kind: Set, Name: "d", Value: -3},
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
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(len(all))
	}
	empty, e := l.Top(0)
	if e != nil || len(empty) != 0 {
		t.Fatal(e, empty)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	top, _ := l.Top(1)
	top[0].Value = 999
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatal("internal state mutated via returned slice")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 16, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			name := fmt.Sprintf("acct-%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Kind: Add, Name: name, Delta: 1}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 32 {
		t.Fatal(len(s.Accounts))
	}
	for _, a := range s.Accounts {
		if a.Value != 50 {
			t.Fatal(a)
		}
	}
	if s.Generation != 32*50 {
		t.Fatal(s.Generation)
	}
}
