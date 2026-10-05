package costledger

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	good := []string{"a", "z0-_", "12345678"}
	for _, n := range good {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndStructuralFirst(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural error later in batch must win over state errors earlier.
	_, e := l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}, {Kind: 0, Name: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal(g)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}, {Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatal(got)
	}
}

func TestAbsLimitAndRevisionRollback(t *testing.T) {
	l := led(t)
	r1, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}}})
	if e != nil || r1.Revision != 1 {
		t.Fatal(e, r1)
	}
	// Failing batch must not consume revisions or generation.
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 1}, {Add, "b", 100, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 2 {
		t.Fatal(s)
	}
	r2, e := l.Apply(Batch{Ops: []Op{{Add, "a", -20, 0}}})
	if e != nil || r2.Revision != 2 || r2.Generation != 2 {
		t.Fatal(e, r2)
	}
}

func TestEmptyBatch(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || r.Changed != nil {
		t.Fatal(e, r)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	r, e = l.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	_, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Set, "d", 0, 4}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 2 {
		t.Fatal(n)
	}
	// Delete-then-create within one batch fits capacity.
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
		t.Fatal(e, top)
	}
	want := []string{"d", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "c" {
		t.Fatal(all)
	}
	empty, _ := l.Top(0)
	if len(empty) != 0 {
		t.Fatal(empty)
	}
}

func TestReturnedSliceIsolation(t *testing.T) {
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
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			name := fmt.Sprintf("acct-%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = l.Top(10)
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
	// Revisions are continuous: 32*50 Add ops, no failures.
	if s.NextRevision != 32*50+1 {
		t.Fatal(s.NextRevision)
	}
	if s.Generation != 32*50 {
		t.Fatal(s.Generation)
	}
}
