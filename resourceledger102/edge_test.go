package resourceledger102

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{MaxAccounts: 0, MaxNameBytes: 8, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 0, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 0},
		{MaxAccounts: -1, MaxNameBytes: 8, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: -5},
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
		{Ops: []Op{{Kind: Add, Name: "toolongname"}}},
		{Ops: []Op{{Kind: Add, Name: "a", Delta: 1}, {Kind: Set, Name: "!"}}},
	}
	for _, b := range bad {
		if _, e := l.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal("failed batches must not change generation", g)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a-z_09", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal(g)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -2, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	small, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := small.Apply(Batch{Ops: []Op{{Set, "a", 0, 11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := small.Apply(Batch{Ops: []Op{{Set, "a", 0, -11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := small.Apply(Batch{Ops: []Op{{Add, "a", 5, 0}, {Add, "a", 6, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if n := len(small.Snapshot().Accounts); n != 0 {
		t.Fatal("rollback failed", n)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 2 || s.Generation != 1 {
		t.Fatal(s)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "c", 0, 3}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionSequence(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Add, "b", 1, 0}}})
	if e != nil || r.Revision != 3 {
		t.Fatal(e, r)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Changed[0].Revision != 3 || r.Changed[0].Value != 3 {
		t.Fatal(r.Changed)
	}
	s := l.Snapshot()
	if s.NextRevision != 4 || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(e, top)
	}
	if top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top)
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "d" {
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

func TestConcurrentStress(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			name := string(rune('a'+i%26)) + string(rune('0'+i/26))
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
	if total != 32*50 {
		t.Fatal(total)
	}
}
