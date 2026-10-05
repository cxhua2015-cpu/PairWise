package resourceledger107

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, 1, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "toolongname", "é", "a.b"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefgh"}
	for _, n := range good {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(0), "a", 0, 1}, {Set, "bad name", 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(99), "a", 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestAbsLimitAndOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 20})
	cases := []Op{
		{Set, "a", 0, 21},
		{Set, "a", 0, -21},
		{Set, "a", 0, math.MinInt64},
		{Add, "a", 21, 0},
		{Add, "a", math.MinInt64, 0},
	}
	for _, op := range cases {
		if _, e := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrValue) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}}}); e != nil {
		t.Fatal(e)
	}
	// 20 + 1 exceeds the limit.
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Overflow of int64 arithmetic itself must be detected before wrapping.
	big, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := big.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := big.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := big.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := big.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	s0 := l.Snapshot()
	if s0.Generation != 0 || s0.NextRevision != 1 {
		t.Fatal(s0)
	}
	// Empty batch succeeds but does not bump the generation.
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if l.Snapshot().Generation != 0 {
		t.Fatal("empty batch changed generation")
	}
	r, e = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	// Failed batch leaves generation and revision counter untouched.
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestChangedOrderAndTopTies(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	r, e := l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Add, "b", 1, 0}, {Set, "c", 0, 5},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Changed) != 3 || r.Changed[0].Name != "b" || r.Changed[0].Value != 6 ||
		r.Changed[1].Name != "a" || r.Changed[2].Name != "c" {
		t.Fatal(r.Changed)
	}
	top, e := l.Top(10)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	if top[0].Name != "b" || top[1].Name != "a" || top[2].Name != "c" {
		t.Fatal(top)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if top, e = l.Top(0); e != nil || len(top) != 0 {
		t.Fatal(top, e)
	}
	// Snapshot sorted by name.
	s := l.Snapshot()
	if s.Accounts[0].Name != "a" || s.Accounts[1].Name != "b" || s.Accounts[2].Name != "c" {
		t.Fatal(s.Accounts)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	top, _ := l.Top(1)
	top[0].Value = 999
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("internal state mutated via returned slice")
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
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Set, k, 0, int64(j)}}})
				_, _ = l.Top(8)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if s.Generation == 0 || len(s.Accounts) == 0 {
		t.Fatal(s)
	}
	// Every successful batch assigns exactly one revision, so the
	// revision counter stays dense: NextRevision-1 == Generation.
	if s.NextRevision != s.Generation+1 {
		t.Fatal(s.NextRevision, s.Generation)
	}
}
