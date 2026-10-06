package balanceledger257

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
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameAndKindValidation(t *testing.T) {
	l := led(t)
	bad := []Op{
		{Add, "", 1, 0},
		{Add, "UPPER", 1, 0},
		{Add, "has space", 1, 0},
		{Add, "toolongname", 1, 0},
		{Add, "a", 0, 0},
		{Kind(99), "a", 1, 0},
	}
	for _, op := range bad {
		if err := l.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Set, "ok_nm-1", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestAbsLimitOnInput(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
}

func TestOverflowDetectedBeforeArithmetic(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatalf("state mutated: %d", got)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "ghost", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if l.Snapshot().Generation != 0 {
		t.Fatal("failed batch bumped generation")
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t) // MaxAccounts 4
	for _, n := range []string{"a", "b", "c", "d"} {
		if _, err := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "e", 0, 1}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if len(l.Snapshot().Accounts) != 4 {
		t.Fatal("capacity failure leaked accounts")
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
	for i, n := range want {
		if top[i].Name != n {
			t.Fatal(top)
		}
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(all)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	top, _ := l.Top(1)
	top[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("returned slices alias internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 1000})
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
				if err := l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	if st.Accounts != 16 || st.Generation != 800 || st.NextRevision != 801 {
		t.Fatalf("%+v", st)
	}
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != st {
		t.Fatal("clone diverged")
	}
}
