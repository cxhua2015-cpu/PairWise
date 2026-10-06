package balanceledger252

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
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongname", "a.b"}
	for _, n := range bad {
		if e := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z0-_", "12345678"} {
		if e := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestStructuralDiscipline(t *testing.T) {
	l := led(t)
	cases := []Op{
		{Kind(0), "a", 1, 0}, {Kind(9), "a", 1, 0}, // unknown kind
		{Add, "a", 0, 0},                         // zero delta
		{Add, "a", 1, 1},                         // extra value field
		{Set, "a", 1, 1},                         // extra delta field
		{Delete, "a", 1, 0}, {Delete, "a", 0, 1}, // extra fields
	}
	for _, op := range cases {
		if e := l.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
}

func TestAbsLimitPrecheck(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if e := l.ValidateBatch(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestOverflowDetectedBeforeArithmetic(t *testing.T) {
	l, e := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Add, "a", -2, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed batches must not consume revisions or generation.
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 3 || s.Accounts[0].Value != math.MinInt64+1 {
		t.Fatalf("%+v", s)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r, e = l.Apply(Batch{})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
}

func TestTopOrderingAndLimit(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -3},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(e, top)
	}
	want := []string{"c", "a", "b"}
	for i, n := range want {
		if top[i].Name != n {
			t.Fatal(top)
		}
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatal(all)
	}
	if _, e = l.Top(0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestReturnedSlicesAreDetached(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 99
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatal("slice aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 12, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 20; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
				_ = l.Stats()
				if c, e := l.Clone(); e == nil {
					_, _ = c.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				}
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	s := l.Snapshot()
	if st.Accounts != len(s.Accounts) || st.Generation != s.Generation || st.NextRevision != s.NextRevision {
		t.Fatalf("stats %+v vs snapshot %+v", st, s)
	}
	// Every successful Add consumed exactly one revision.
	if s.NextRevision != s.Generation+1 {
		t.Fatalf("revisions not consecutive: %+v", s)
	}
}
