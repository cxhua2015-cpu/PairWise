package resourceledger087

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0},
		{-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	l := led(t)
	cases := []Op{
		{Kind(0), "a", 0, 0},
		{Kind(9), "a", 0, 0},
		{Add, "", 1, 0},
		{Add, "A", 1, 0},
		{Add, "a b", 1, 0},
		{Add, "toolongname", 1, 0},
		{Add, "a", 1, 1},
		{Set, "a", 1, 1},
		{Delete, "a", 1, 0},
		{Delete, "a", 0, 1},
	}
	for _, op := range cases {
		if _, e := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestAbsLimitAndOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 11, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 10}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != 10 {
		t.Fatal(got)
	}
}

func TestOverflowGuard(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -math.MaxInt64}, {Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatal(got)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	b := l.Snapshot()
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, l.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestEmptyBatch(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	r, e = l.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	l := led(t)
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Set, "a", 0, 2}}})
	if r.Revision != 2 || r.Generation != 1 || len(r.Changed) != 1 || r.Changed[0].Revision != 2 {
		t.Fatal(r)
	}
	s := l.Snapshot()
	if s.NextRevision != 3 || s.Generation != 1 {
		t.Fatal(s)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if l.Snapshot().Generation != 1 {
		t.Fatal("generation changed on failed batch")
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
	want := []string{"c", "a", "b"}
	for i, name := range want {
		if top[i].Name != name {
			t.Fatal(top)
		}
	}
	all, _ := l.Top(64)
	if len(all) != 4 {
		t.Fatal(all)
	}
	zero, _ := l.Top(0)
	if len(zero) != 0 {
		t.Fatal(zero)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestReturnedIsolation(t *testing.T) {
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
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			name := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
			}
			_, _ = l.Apply(Batch{Ops: []Op{{Delete, name, 0, 0}, {Set, name, 0, 1}}})
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 32 {
		t.Fatal(len(s.Accounts))
	}
	for i := 1; i < len(s.Accounts); i++ {
		if s.Accounts[i-1].Name >= s.Accounts[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}
