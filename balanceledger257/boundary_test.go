package balanceledger257

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

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Add, "", 1, 0}}},
		{Ops: []Op{{Add, "A", 1, 0}}},
		{Ops: []Op{{Add, "a b", 1, 0}}},
		{Ops: []Op{{Add, "toolongname", 1, 0}}},
		{Ops: []Op{{Add, "a", 0, 0}}},
		{Ops: []Op{{Add, "a", 1, 1}}},
		{Ops: []Op{{Set, "a", 1, 1}}},
		{Ops: []Op{{Delete, "a", 1, 0}}},
		{Ops: []Op{{Delete, "a", 0, 1}}},
	}
	for i, b := range bad {
		if err := l.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := l.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "ok-1_", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal("ValidateBatch mutated state")
	}
}

func TestOverflowGuards(t *testing.T) {
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
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatal("overflow mutated state", got)
	}
}

func TestAbsLimitAndRollback(t *testing.T) {
	l := led(t)
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}, {Add, "a", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal("failed batch leaked state", n)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Add, "a", 1, 0}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 1 || r.Revision != 3 {
		t.Fatal(r)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[0].Revision != 3 || r.Changed[1].Revision != 2 {
		t.Fatal(r.Changed)
	}
	g := r.Generation
	r2, err := l.Apply(Batch{})
	if err != nil || r2.Generation != g {
		t.Fatal("empty batch changed generation", r2, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if l.Snapshot().Generation != g {
		t.Fatal("failed batch bumped generation")
	}
	s := l.Stats()
	if s.Generation != g || s.Accounts != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -3},
	}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 {
		t.Fatal(top, err)
	}
	want := []string{"c", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
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

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("snapshot aliases internal state")
	}
	top, _ := l.Top(1)
	top[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("top aliases internal state")
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 3}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Generation != 1 || c.Stats().NextRevision != 2 {
		t.Fatal("clone lost logical clocks")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "z", 0, 1}}})
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 1 {
		t.Fatal("clone not isolated")
	}
	if l.Snapshot().Accounts[0].Name != "a" || c.Snapshot().Accounts[0].Name != "z" {
		t.Fatal("clone not isolated")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 1000})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Add, k, 1, 0}}})
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	wg.Wait()
	s := l.Stats()
	if s.Accounts != 16 || s.Generation != 800 {
		t.Fatal(s)
	}
	for _, a := range l.Snapshot().Accounts {
		if a.Value != 50 {
			t.Fatal(a)
		}
	}
}
