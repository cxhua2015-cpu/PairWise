package balanceledger277

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestOptionsAndNames(t *testing.T) {
	if _, e := New(Options{0, 1, 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{1, 1, 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	l := led(t)
	bad := []Op{
		{Add, "", 1, 0},
		{Add, "A", 1, 0},
		{Add, "a b", 1, 0},
		{Add, "toolongname", 1, 0},
		{Add, "a", 0, 0},
		{Add, "a", 1, 1},
		{Set, "a", 1, 1},
		{Set, "a", 0, 21},
		{Delete, "a", 1, 0},
		{Kind(9), "a", 1, 0},
	}
	for _, op := range bad {
		if e := l.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Add, "ok-1_", 20, 0}, {Set, "b", 0, -20}, {Delete, "c", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}, {Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Accounts[0].Value != math.MaxInt64 || s.Generation != 1 {
		t.Fatalf("rollback broken: %+v", s)
	}
	l2, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 9}, {Add, "a", 2, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestCapacityRollbackAndGeneration(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("rollback broken: %+v", s)
	}
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if l.Stats().Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Add, "a", 1, 0}, {Delete, "a", 0, 0}}})
	if len(r.Changed) != 0 || r.Revision != 2 {
		t.Fatalf("changed/revision: %+v", r)
	}
}

func TestTopOrderAndIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3}}})
	top, e := l.Top(2)
	if e != nil || len(top) != 2 || top[0].Name != "a" || top[1].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e = l.Top(0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	top[0].Value = -99
	if l.Snapshot().Accounts[0].Value != 5 {
		t.Fatal("returned slices alias state")
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
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				if j == 25 {
					c, err := l.Clone()
					if err == nil {
						_, _ = c.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
					}
				}
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	if st.Accounts != 16 || st.Generation != 16*50 || st.NextRevision != 16*50+1 {
		t.Fatalf("stats: %+v", st)
	}
	var sum int64
	for _, a := range l.Snapshot().Accounts {
		sum += a.Value
	}
	if sum != 16*50 {
		t.Fatal(sum)
	}
}
