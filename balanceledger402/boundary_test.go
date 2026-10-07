package balanceledger402

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
	bad := []Op{
		{Kind(0), "a", 1, 0},
		{Kind(9), "a", 1, 0},
		{Add, "", 1, 0},
		{Add, "A", 1, 0},
		{Add, "a b", 1, 0},
		{Add, "toolongname", 1, 0},
		{Add, "a", 0, 0},
		{Add, "a", 1, 1},
		{Set, "a", 1, 1},
		{Delete, "a", 1, 0},
		{Delete, "a", 0, 1},
	}
	for _, op := range bad {
		if err := l.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	good := []Op{
		{Add, "a", 1, 0}, {Add, "z-9_", -1, 0},
		{Set, "a", 0, 0}, {Set, "a", 0, -20}, {Delete, "a", 0, 0},
	}
	for _, op := range good {
		if err := l.ValidateBatch(Batch{Ops: []Op{op}}); err != nil {
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
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if got := l.Snapshot().Accounts[1].Value; got != math.MinInt64+1 {
		t.Fatal(got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = l.Apply(Batch{})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
}

func TestDeleteNotFoundAndReadd(t *testing.T) {
	l := led(t)
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "ghost", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 5}, {Delete, "a", 0, 0}, {Add, "a", 2, 0}}}); err != nil {
		t.Fatal(err)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != 2 || s.Accounts[0].Revision != 2 {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
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

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 99
	r, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	r.Changed[0].Value = 99
	if got := l.Snapshot().Accounts[0].Value; got != 2 {
		t.Fatal(got)
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
		t.Fatal(c.Stats(), l.Stats())
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 0 {
		t.Fatal("clone shares state")
	}
	if l.Stats().NextRevision != c.Stats().NextRevision-0 && l.Stats().Generation == c.Stats().Generation {
		t.Fatal("clocks not preserved/independent")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, name, 0, 1}}})
			}
		}()
	}
	wg.Wait()
	st := l.Stats()
	if st.Accounts != 16 || st.NextRevision != 16*50+1 || st.Generation != 16*50 {
		t.Fatalf("%+v", st)
	}
	var total int64
	for _, a := range l.Snapshot().Accounts {
		total += a.Value
	}
	if total != 16*50 {
		t.Fatal(total)
	}
}

func TestConcurrentCloneAndApply(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				c, err := l.Clone()
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = c.Apply(Batch{Ops: []Op{{Add, "x", 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Add, "y", 1, 0}}})
			}
		}()
	}
	wg.Wait()
	if got := l.Snapshot().Accounts[0].Value; got != 8*30 {
		t.Fatal(got)
	}
}
