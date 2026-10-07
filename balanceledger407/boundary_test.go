package balanceledger407

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

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	cases := []struct {
		op   Op
		want error
	}{
		{Op{Kind: 0, Name: "a", Delta: 1}, ErrInvalidInput},
		{Op{Kind: 99, Name: "a", Delta: 1}, ErrInvalidInput},
		{Op{Kind: Add, Name: "", Delta: 1}, ErrInvalidInput},
		{Op{Kind: Add, Name: "A", Delta: 1}, ErrInvalidInput},
		{Op{Kind: Add, Name: "a b", Delta: 1}, ErrInvalidInput},
		{Op{Kind: Add, Name: "toolongname", Delta: 1}, ErrInvalidInput},
		{Op{Kind: Add, Name: "a"}, ErrInvalidInput},                     // zero delta
		{Op{Kind: Add, Name: "a", Delta: 1, Value: 1}, ErrInvalidInput}, // extra field
		{Op{Kind: Set, Name: "a", Delta: 1}, ErrInvalidInput},
		{Op{Kind: Delete, Name: "a", Delta: 1}, ErrInvalidInput},
		{Op{Kind: Add, Name: "a", Delta: 21}, ErrValue},  // beyond MaxAbsValue
		{Op{Kind: Set, Name: "a", Value: -21}, ErrValue}, // beyond MaxAbsValue
		{Op{Kind: Add, Name: "ok-_1", Delta: 1}, nil},
		{Op{Kind: Set, Name: "ok-_1", Value: -20}, nil},
		{Op{Kind: Delete, Name: "ok-_1"}, nil},
	}
	for _, c := range cases {
		if e := l.ValidateBatch(Batch{Ops: []Op{c.op}}); !errors.Is(e, c.want) && e != c.want {
			t.Fatalf("op %+v: got %v want %v", c.op, e, c.want)
		}
	}
	// ValidateBatch must not mutate state.
	if s := l.Snapshot(); s.Generation != 0 || len(s.Accounts) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestOverflowBeforeArithmetic(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("expected overflow ErrValue, got %v", e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -2, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("expected underflow ErrValue, got %v", e)
	}
	// State must be untouched after failed batches.
	if got := l.Snapshot().Accounts[0].Value; got != math.MinInt64+1 {
		t.Fatalf("state changed: %d", got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{}) // empty batch: no generation bump
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatalf("empty batch: %+v %v", r, e)
	}
	r, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatalf("got %+v %v", r, e)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[1].Name != "b" {
		t.Fatalf("changed: %+v", r.Changed)
	}
	// Failed batch leaves clocks alone.
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := l.Stats(); s.Generation != 1 || s.NextRevision != 3 || s.Accounts != 1 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestChangedMergesRepeatedOps(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 3, 0}, {Add, "a", 2, 0}, {Set, "a", 0, 7}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Changed) != 1 || r.Changed[0].Value != 7 || r.Changed[0].Revision != 3 {
		t.Fatalf("changed: %+v", r.Changed)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -1}, {Set, "d", 0, 9},
	}})
	top, e := l.Top(3)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"d", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatalf("top[%d]=%s want %s", i, top[i].Name, w)
		}
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("negative n: %v", e)
	}
	if top, e = l.Top(0); e != nil || len(top) != 0 {
		t.Fatalf("zero n: %v %v", top, e)
	}
}

func TestReturnedSlicesAreIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 99
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatalf("internal state mutated via returned slice: %d", got)
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 5}}})
	c, e := l.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	if _, e = c.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if l.Snapshot().Accounts[0].Value != 5 || c.Snapshot().Accounts[0].Value != 6 {
		t.Fatal("clone shares ownership with original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
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
	w.Wait()
	s := l.Stats()
	if s.Accounts != 16 || s.NextRevision != 801 || s.Generation != 800 {
		t.Fatalf("stats: %+v", s)
	}
	c, e := l.Clone()
	if e != nil || c.Stats() != s {
		t.Fatalf("clone: %+v %v", c.Stats(), e)
	}
}
