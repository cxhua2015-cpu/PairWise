package balanceledger412

import (
	"errors"
	"fmt"
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
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"}
	for _, n := range bad {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	good := []string{"a", "z0-_", "12345678"}
	for _, n := range good {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
}

func TestStructuralPreflight(t *testing.T) {
	l := led(t)
	if err := l.ValidateBatch(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 1, 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "a", 0, 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Set, "a", 0, 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	// Preflight must not mutate state.
	if s := l.Stats(); s.Accounts != 0 || s.Generation != 0 {
		t.Fatalf("preflight mutated state: %+v", s)
	}
}

func TestOverflowCheckedBeforeArithmetic(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", -2, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	// State must be intact after the rejected overflow attempts.
	if got := l.Snapshot().Accounts[0].Value; got != math.MinInt64+1 {
		t.Fatal(got)
	}
}

func TestAbsLimitOnSet(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	r, err = l.Apply(Batch{Ops: []Op{}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	// Net-zero churn inside the batch is fine, but ending above capacity fails.
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "c", 0, 3}, {Set, "d", 0, 4}, {Set, "a", 0, 1}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := l.Snapshot(); got.Generation != before.Generation || len(got.Accounts) != 2 {
		t.Fatalf("state changed after rollback: %+v", got)
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
	for i, w := range want {
		if top[i].Name != w {
			t.Fatalf("top[%d]=%s want %s", i, top[i].Name, w)
		}
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if got, _ := l.Top(0); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestReturnedSlicesAreDetached(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	snap := l.Snapshot()
	snap.Accounts[0].Value = 999
	top, _ := l.Top(1)
	top[0].Value = 999
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatal(got)
	}
}

func TestCloneIndependentClocks(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone diverged at copy time")
	}
	r, _ := c.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}}})
	if r.Revision != 2 || r.Generation != 2 {
		t.Fatalf("clone lost logical clock: %+v", r)
	}
	if l.Stats().Accounts != 1 || l.Stats().Generation != 1 {
		t.Fatal("original affected by clone mutation")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 16, MaxAbsValue: 1 << 40})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("acct-%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, name, 0, 1}}})
				_, _ = l.Top(8)
				_ = l.Snapshot()
				_ = l.Stats()
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	wg.Wait()
	st := l.Stats()
	if st.Accounts != 32 || st.Generation != 32*50 || st.NextRevision != 32*50+1 {
		t.Fatalf("stats: %+v", st)
	}
	for _, acc := range l.Snapshot().Accounts {
		if acc.Value != 50 {
			t.Fatalf("%s=%d", acc.Name, acc.Value)
		}
	}
}
