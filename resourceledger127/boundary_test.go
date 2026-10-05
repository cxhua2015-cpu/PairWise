package resourceledger127

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {-1, 8, 10}, {4, 8, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongname", "a.b", "+x"}
	for _, n := range bad {
		_, e := l.Apply(Batch{Ops: []Op{{Add, n, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
	good := []string{"a", "z-0_9", "12345678"}
	for _, n := range good {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	for _, k := range []Kind{0, 4, 255} {
		if _, e := l.Apply(Batch{Ops: []Op{{k, "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: got %v", k, e)
		}
	}
}

func TestAbsLimitAndOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 20})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Cumulative add exceeding the limit fails and rolls back.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 15}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 6, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != 15 {
		t.Fatalf("rollback failed, value=%d", got)
	}
	// Boundary values exactly at the limit succeed.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Set, "a", 0, -20}}}); e != nil {
		t.Fatal(e)
	}
}

func TestOverflowDetection(t *testing.T) {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -2, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// MinInt64 as delta is rejected by the abs limit (|MinInt64| unrepresentable).
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r0, e := l.Apply(Batch{})
	if e != nil || r0.Generation != 0 {
		t.Fatal(r0, e)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 || len(r1.Changed) != 1 || r1.Changed[0].Name != "b" {
		t.Fatal(r1)
	}
	// Failed batch must not bump generation or consume revisions.
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	r2, _ := l.Apply(Batch{Ops: []Op{{Add, "b", 1, 0}}})
	if r2.Generation != 2 || r2.Revision != 3 || r2.Changed[0].Revision != 3 {
		t.Fatal(r2)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	// Net-zero intermediate deletes still exceed capacity at batch end.
	_, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 2 {
		t.Fatalf("rollback failed, accounts=%d", n)
	}
	// Delete-then-create within the same batch fits.
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "c", 0, 3}}}); e != nil {
		t.Fatal(e)
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
	if _, e = l.Top(0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatal(len(all))
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("snapshot shares state with ledger")
	}
	top, _ := l.Top(1)
	top[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("top shares state with ledger")
	}
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}}})
	r.Changed[0].Value = 999
	if l.Snapshot().Accounts[1].Value != 2 {
		t.Fatal("result shares state with ledger")
	}
}

func TestConcurrentStress(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 16, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			name := fmt.Sprintf("acct-%02d", g)
			for i := 0; i < 200; i++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 8 {
		t.Fatal(len(s.Accounts))
	}
	for _, a := range s.Accounts {
		if a.Value != 200 {
			t.Fatalf("%s=%d want 200", a.Name, a.Value)
		}
	}
	if s.Generation != 8*200 || s.NextRevision != 8*200+1 {
		t.Fatal(s.Generation, s.NextRevision)
	}
}
