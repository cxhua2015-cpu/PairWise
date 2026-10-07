package balanceledger427

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {1, 1, -1}, {-1, 1, 1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 4, MaxAbsValue: 10})
	bad := []string{"", "A", "a b", "abcde", "a.b", "é", "UP"}
	for _, n := range bad {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	good := []string{"a", "z-9_", "0", "____"}
	for _, n := range good {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
}

func TestStructuralDiscipline(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 10})
	cases := []Op{
		{Kind(0), "a", 1, 0}, {Kind(9), "a", 1, 0}, // unknown kind
		{Add, "a", 0, 0},                         // zero delta
		{Add, "a", 1, 1},                         // extra value
		{Set, "a", 1, 1},                         // extra delta
		{Delete, "a", 1, 0}, {Delete, "a", 0, 1}, // extra fields
	}
	for _, op := range cases {
		if err := l.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("overflow: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("min abs: %v", err)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != math.MaxInt64 {
		t.Fatalf("state mutated: %+v", s)
	}

	small, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 5})
	if _, err := small.Apply(Batch{Ops: []Op{{Set, "a", 0, 6}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("set abs: %v", err)
	}
	if _, err := small.Apply(Batch{Ops: []Op{{Set, "a", 0, -5}, {Add, "a", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("add abs: %v", err)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || r.Changed != nil {
		t.Fatalf("empty: %+v %v", r, err)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatalf("first: %+v", r)
	}
	r, _ = l.Apply(Batch{})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatalf("empty after: %+v", r)
	}
}

func TestRevisionSequenceAndChanged(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	r, err := l.Apply(Batch{Ops: []Op{
		{Set, "a", 0, 1}, {Add, "a", 2, 0}, {Set, "b", 0, 5}, {Delete, "b", 0, 0},
	}})
	if err != nil || r.Revision != 3 || len(r.Changed) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Changed[0].Name != "a" || r.Changed[0].Value != 3 || r.Changed[0].Revision != 2 {
		t.Fatalf("changed a: %+v", r.Changed[0])
	}
	if r.Changed[1].Name != "b" || r.Changed[1].Revision != 3 {
		t.Fatalf("changed b: %+v", r.Changed[1])
	}
	s := l.Snapshot()
	if s.NextRevision != 4 || len(s.Accounts) != 1 {
		t.Fatalf("snapshot: %+v", s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 7}, {Set, "d", 0, -1}}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatalf("top: %+v %v", top, err)
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("top -1: %v", err)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatalf("top cap: %d", len(all))
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r.Changed[0].Value = 999
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	top, _ := l.Top(1)
	top[0].Value = 999
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatalf("aliased: %d", got)
	}
}

func TestCloneClocksAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clocks differ")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if len(l.Snapshot().Accounts) != 1 || len(c.Snapshot().Accounts) != 0 {
		t.Fatal("clone not isolated")
	}
}

func TestPreviewFailurePriority(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 5})
	b := Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Delete, "zz", 0, 0}}}
	_, _, _, perr := l.Preview(b)
	_, aerr := l.Apply(b)
	if perr != aerr || !errors.Is(perr, ErrNotFound) {
		t.Fatalf("priority: preview=%v apply=%v", perr, aerr)
	}
	// Capacity checked only at batch end.
	l2, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 5})
	_, _, _, err := l2.Preview(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	if len(l2.Snapshot().Accounts) != 0 {
		t.Fatal("failed preview mutated state")
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
			k := string(rune('a' + i%26))
			switch i % 6 {
			case 0:
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
			case 1:
				_, _ = l.Top(10)
			case 2:
				_ = l.Snapshot()
			case 3:
				_ = l.Stats()
			case 4:
				_, _, _, _ = l.Preview(Batch{Ops: []Op{{Add, k, 1, 0}}})
			case 5:
				c, _ := l.Clone()
				if c != nil {
					_ = c.Snapshot()
				}
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	s := l.Snapshot()
	if st.Accounts != len(s.Accounts) || st.Generation != s.Generation || st.NextRevision != s.NextRevision {
		t.Fatalf("inconsistent: %+v vs %+v", st, s)
	}
}
