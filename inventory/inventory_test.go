package inventory

import (
	"errors"
	"math"
	"reflect"
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

func TestInvalidInput(t *testing.T) {
	l := led(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Kind: Add, Name: ""}}},
		{Ops: []Op{{Kind: Add, Name: "Upper"}}},
		{Ops: []Op{{Kind: Add, Name: "has space"}}},
		{Ops: []Op{{Kind: Add, Name: "dot.com"}}},
		{Ops: []Op{{Kind: Add, Name: "toolongname"}}}, // > 8 bytes
		{Ops: []Op{{Kind: Add, Name: "a"}, {Kind: Delete, Name: "ok"}, {Kind: 7, Name: "x"}}},
	}
	for _, b := range bad {
		if _, e := l.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	// Structural validation happens before state reads: nothing changed.
	if s := l.Snapshot(); s.Generation != 0 || len(s.Accounts) != 0 {
		t.Fatal(s)
	}
}

func TestValidNames(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{
		{Set, "abc-09_x", 0, 1},
	}}); e != nil {
		t.Fatal(e)
	}
}

func TestOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e) // |MinInt64| exceeds MaxAbsValue
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed batches rolled back.
	s := l.Snapshot()
	if s.Accounts[0].Value != math.MaxInt64 || s.Accounts[1].Value != math.MinInt64+1 {
		t.Fatal(s)
	}
}

func TestAbsLimit(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t) // MaxAccounts 4
	ops := []Op{}
	for _, n := range []string{"a", "b", "c", "d"} {
		ops = append(ops, Op{Set, n, 0, 1})
	}
	if _, e := l.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	before := l.Snapshot()
	// Net-new account at batch end exceeds capacity -> rollback.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "e", 0, 1}, {Set, "f", 0, 1}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r) // Delete allocates no revision
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Changed[0].Revision != 2 {
		t.Fatal(r.Changed)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	// Failed batch does not bump generation.
	_, _ = l.Apply(Batch{Ops: []Op{{Delete, "nope", 0, 0}}})
	if l.Snapshot().Generation != 1 {
		t.Fatal("generation bumped on failure")
	}
}

func TestChangedOrderAndFinalValues(t *testing.T) {
	l := led(t)
	r, _ := l.Apply(Batch{Ops: []Op{
		{Add, "b", 1, 0}, {Add, "a", 1, 0}, {Add, "b", 4, 0},
	}})
	if len(r.Changed) != 2 || r.Changed[0].Name != "b" || r.Changed[1].Name != "a" {
		t.Fatal(r.Changed)
	}
	if r.Changed[0].Value != 5 || r.Changed[0].Revision != 3 {
		t.Fatal(r.Changed[0])
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	want := []string{"c", "a", "b"} // value desc, name asc on ties
	for i, n := range want {
		if top[i].Name != n {
			t.Fatal(top)
		}
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
	s.Accounts[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 99
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatal(got)
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
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 16 {
		t.Fatal(len(s.Accounts))
	}
	for _, a := range s.Accounts {
		if a.Value != 50 {
			t.Fatal(a)
		}
	}
	if s.Generation != 800 || s.NextRevision != 801 {
		t.Fatal(s)
	}
}
