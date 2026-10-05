package resourceledger122

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func opts() Options { return Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100} }

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l, _ := New(opts())
	bad := []Op{
		{Kind: 0, Name: "a"},
		{Kind: 99, Name: "a"},
		{Add, "", 1, 0},
		{Add, "A", 1, 0},
		{Add, "a b", 1, 0},
		{Add, "toolongname", 1, 0},
		{Add, "a", 1, 1},    // extra Value
		{Set, "a", 1, 1},    // extra Delta
		{Delete, "a", 1, 0}, // extra Delta
		{Delete, "a", 0, 1}, // extra Value
	}
	for _, op := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	// Whole batch validated before state reads: second op invalid -> no effect.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "ok", 0, 1}, {Kind: 7, Name: "x"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(l.Snapshot().Accounts) != 0 {
		t.Fatal("state mutated by invalid batch")
	}
	// Valid name characters.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a-z_0-9", 0, 1}}}); e != nil {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e) // abs(MinInt64) exceeds limit
	}
	l2, _ := New(opts())
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "a", 101, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, -101}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// In-batch accumulation beyond limit must fail and roll back.
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "a", 60, 0}, {Add, "a", 60, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if len(l2.Snapshot().Accounts) != 0 {
		t.Fatal("rollback failed")
	}
}

func TestCapacityRollbackAndGeneration(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 10})
	r, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
	g := l.Snapshot().Generation
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != g || len(s.Accounts) != 1 || s.Accounts[0].Name != "a" {
		t.Fatal("rollback failed", s)
	}
	// Empty batch: no error, generation unchanged.
	r, e = l.Apply(Batch{})
	if e != nil || r.Generation != 0 || l.Snapshot().Generation != g {
		t.Fatal(e, r)
	}
}

func TestRevisionAndChanged(t *testing.T) {
	l, _ := New(opts())
	r, e := l.Apply(Batch{Ops: []Op{
		{Add, "a", 1, 0}, {Add, "a", 1, 0}, {Set, "b", 0, 5}, {Delete, "b", 0, 0}, {Set, "b", 0, 7},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Revision != 4 || len(r.Changed) != 2 {
		t.Fatal(r)
	}
	if r.Changed[0].Name != "a" || r.Changed[0].Revision != 2 || r.Changed[0].Value != 2 {
		t.Fatal(r.Changed[0])
	}
	if r.Changed[1].Name != "b" || r.Changed[1].Revision != 4 || r.Changed[1].Value != 7 {
		t.Fatal(r.Changed[1])
	}
	s := l.Snapshot()
	if s.NextRevision != 5 || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(opts())
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(e, top)
	}
	want := []string{"c", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatal(all)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Returned slice is isolated from internal state.
	top[0].Value = -999
	if again, _ := l.Top(1); again[0].Value != 9 {
		t.Fatal("slice aliases internal state")
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
			name := string(rune('a'+i%8)) + "x"
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	var total int64
	for _, a := range s.Accounts {
		total += a.Value
	}
	if total != 16*50 {
		t.Fatal(total)
	}
	if s.Generation != 16*50 {
		t.Fatal(s.Generation)
	}
}
