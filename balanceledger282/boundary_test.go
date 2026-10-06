package balanceledger282

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestValidation(t *testing.T) {
	l := led(t)
	bad := []Op{
		{Kind: 0, Name: "a"},
		{Kind: 9, Name: "a"},
		{Add, "", 1, 0},
		{Add, "A", 1, 0},
		{Add, "a b", 1, 0},
		{Add, "toolongname", 1, 0},
		{Add, "a", 0, 0},
		{Add, "a", 1, 1},
		{Set, "a", 1, 0},
		{Delete, "a", 1, 0},
		{Delete, "a", 0, 1},
	}
	for _, op := range bad {
		if err := l.ValidateBatch(Batch{Ops: []Op{op}}); err != ErrInvalidInput {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "ok-1_", 1, 0}, {Set, "b", 0, 0}, {Delete, "c", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if err := l.ValidateBatch(Batch{}); err != nil {
		t.Fatal(err)
	}
}

func TestOverflowAndAbs(t *testing.T) {
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
	l2, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, -10}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || r.Changed != nil {
		t.Fatal(r, e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = l.Apply(Batch{})
	if r.Generation != 1 {
		t.Fatal(r)
	}
}

func TestChangedOrderAndDelete(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}, {Set, "a", 0, 1}, {Add, "b", 3, 0}, {Delete, "a", 0, 0}}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Changed[0].Value != 5 || r.Changed[0].Revision != 3 {
		t.Fatal(r, e)
	}
}

func TestTopAndSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 1}, {Set, "c", 0, 9}}})
	top, e := l.Top(2)
	if e != nil || len(top) != 2 || top[0].Name != "c" || top[1].Name != "a" {
		t.Fatal(top, e)
	}
	if _, e = l.Top(-1); e != ErrInvalidInput {
		t.Fatal(e)
	}
	top[0].Value = -99
	s := l.Snapshot()
	s.Accounts[0].Value = -99
	if l.Snapshot().Accounts[0].Value == -99 {
		t.Fatal("snapshot aliases state")
	}
	names := []string{l.Snapshot().Accounts[0].Name, l.Snapshot().Accounts[1].Name, l.Snapshot().Accounts[2].Name}
	if names[0] != "a" || names[1] != "b" || names[2] != "c" {
		t.Fatal(names)
	}
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {1, 1, -1}} {
		if _, e := New(o); e != ErrInvalidOptions {
			t.Fatal(o, e)
		}
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
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
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := l.Stats()
	if s.Accounts != 8 || s.Generation != 400 || s.NextRevision != 401 {
		t.Fatal(s)
	}
}
