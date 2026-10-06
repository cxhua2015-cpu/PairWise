package balanceledger287

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, 1, -5},
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
	good := []string{"a", "z9-_", "0", "abc-d_0"}
	for _, n := range good {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
}

func TestStructuralErrors(t *testing.T) {
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
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
}

func TestOverflowBeforeArithmetic(t *testing.T) {
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
	// Failed overflow attempts must roll back: value stays MaxInt64-1... actually MinInt64+1.
	if got := l.Snapshot().Accounts[0].Value; got != math.MinInt64+1 {
		t.Fatal(got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	l := led(t)
	r1, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if err != nil || r1.Generation != 1 {
		t.Fatal(r1, err)
	}
	r2, err := l.Apply(Batch{})
	if err != nil || r2.Generation != 1 || r2.Changed != nil {
		t.Fatal(r2, err)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := l.Snapshot(); got.Generation != before.Generation || len(got.Accounts) != 2 {
		t.Fatal(got)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 {
		t.Fatal(top, err)
	}
	if top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top)
	}
	top[0].Value = -999
	if l.Snapshot().Accounts[2].Name != "c" {
		t.Fatal("snapshot order")
	}
	for _, a := range l.Snapshot().Accounts {
		if a.Name == "c" && a.Value != 9 {
			t.Fatal("returned slice aliases internal state")
		}
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestDeleteMissingAndRevisions(t *testing.T) {
	l := led(t)
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "ghost", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Add, "b", 1, 0}}})
	if err != nil || r.Revision != 3 {
		t.Fatal(r, err)
	}
	s := l.Snapshot()
	if s.NextRevision != 4 || len(s.Accounts) != 1 || s.Accounts[0].Revision != 3 {
		t.Fatal(s)
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 7}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clocks not preserved")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 0 {
		t.Fatal("clone shares ownership")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a'+i%26)) + string(rune('0'+i/26))
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
	if st.Accounts == 0 || int(st.Accounts) != len(l.Snapshot().Accounts) {
		t.Fatal(st)
	}
	if st.NextRevision-st.Generation < 1 {
		t.Fatal(st)
	}
	c, err := l.Clone()
	if err != nil || c.Stats() != l.Stats() {
		t.Fatal(err)
	}
}
