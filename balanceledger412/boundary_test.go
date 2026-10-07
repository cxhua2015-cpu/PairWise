package balanceledger412

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
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a.b", "é", "toolongname", "a/b"}
	for _, n := range bad {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	for _, n := range []string{"a", "0", "-", "_", "a-b_c9", "12345678"} {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	cases := []Op{
		{Kind(0), "a", 1, 0},  // unknown kind
		{Kind(99), "a", 1, 0}, // unknown kind
		{Add, "a", 0, 0},      // zero delta
		{Add, "a", 1, 1},      // extra Value field
		{Set, "a", 1, 1},      // extra Delta field
		{Delete, "a", 1, 0},   // extra Delta field
		{Delete, "a", 0, 1},   // extra Value field
	}
	for _, op := range cases {
		if err := l.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	if err := l.ValidateBatch(Batch{}); err != nil {
		t.Fatal(err)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	s := l.Snapshot()
	if s.Accounts[0].Value != math.MaxInt64 || s.Accounts[1].Value != math.MinInt64+1 {
		t.Fatal(s)
	}
}

func TestAbsLimitEnforced(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}}}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
	if _, err := l.Apply(Batch{}); err != nil {
		t.Fatal(err)
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal("empty batch bumped generation")
	}
	r, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "ghost", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if g := l.Snapshot().Generation; g != 1 {
		t.Fatal("failed batch bumped generation")
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 {
		t.Fatal(top, err)
	}
	want := []string{"c", "a", "b"}
	for i, name := range want {
		if top[i].Name != name {
			t.Fatal(top)
		}
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "d" {
		t.Fatal(all)
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 3}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clocks not preserved")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "z", 0, 1}}})
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 1 {
		t.Fatal(l.Stats(), c.Stats())
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); err != nil {
		t.Fatal("original affected by clone")
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
				_, _ = l.Top(4)
				_ = l.Snapshot()
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	if st.Accounts != 16 || st.Generation != 800 || st.NextRevision != 801 {
		t.Fatal(st)
	}
}
