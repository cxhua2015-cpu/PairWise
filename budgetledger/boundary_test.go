package budgetledger

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {-1, 8, 10}, {4, 8, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongname", "a.b", "UPPER"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: n, Value: 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
	good := []string{"a", "z-0_", "12345678", "-"}
	for _, n := range good {
		if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: n, Value: 1}}}); e != nil {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Kind: Add, Name: "a", Delta: 1, Value: 1}}},
		{Ops: []Op{{Kind: Set, Name: "a", Delta: 1, Value: 1}}},
		{Ops: []Op{{Kind: Delete, Name: "a", Delta: 1}}},
		{Ops: []Op{{Kind: Delete, Name: "a", Value: 1}}},
		{Ops: []Op{{Kind: Set, Name: "ok", Value: 1}, {Kind: 7, Name: "bad"}}},
	}
	for _, b := range cases {
		if _, e := l.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: got %v", b, e)
		}
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatalf("generation moved after invalid batches: %d", g)
	}
}

func TestOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Add, Name: "a", Delta: 1}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("positive overflow: got %v", e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("MinInt64 exceeds any positive abs limit: got %v", e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Add, Name: "a", Delta: -1}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("negative overflow: got %v", e)
	}
	// MinInt64 delta on zero value must not wrap.
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Add, Name: "b", Delta: math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("min delta: got %v", e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MinInt64+1 {
		t.Fatalf("state mutated by failed batch: %d", got)
	}
}

func TestAbsLimitAndCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: -11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Capacity exceeded only at batch end: three accounts mid-batch is fine
	// if one is deleted before commit.
	x, e := l.Apply(Batch{Ops: []Op{
		{Kind: Set, Name: "a", Value: 1},
		{Kind: Set, Name: "b", Value: 2},
		{Kind: Set, Name: "c", Value: 3},
		{Kind: Delete, Name: "a"},
	}})
	if e != nil || len(l.Snapshot().Accounts) != 2 {
		t.Fatal(e, l.Snapshot())
	}
	if x.Changed[0].Name != "b" || len(x.Changed) != 2 {
		t.Fatalf("changed: %+v", x.Changed)
	}
	// Exceeding capacity at end rolls back everything.
	before := l.Snapshot()
	if _, e = l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "d", Value: 4}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := l.Snapshot(); got.Generation != before.Generation || len(got.Accounts) != 2 {
		t.Fatal("capacity failure mutated state")
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 {
		t.Fatal(x, e)
	}
	x, _ = l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 1}}})
	if x.Generation != 1 || x.Revision != 1 {
		t.Fatal(x)
	}
	x, e = l.Apply(Batch{Ops: []Op{}})
	if e != nil || x.Generation != 1 || x.Revision != 1 {
		t.Fatal(x, e)
	}
	if s := l.Snapshot(); s.Generation != 1 || s.NextRevision != 2 {
		t.Fatal(s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Kind: Set, Name: "b", Value: 5},
		{Kind: Set, Name: "a", Value: 5},
		{Kind: Set, Name: "c", Value: 9},
		{Kind: Set, Name: "d", Value: -3},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "d" {
		t.Fatal(all)
	}
	// Mutating the returned slice must not affect internal state.
	all[0].Value = -999
	again, _ := l.Top(1)
	if again[0].Value != 9 {
		t.Fatal("returned slice aliases internal state")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "b", Value: 1}, {Kind: Set, Name: "a", Value: 2}}})
	s := l.Snapshot()
	if s.Accounts[0].Name != "a" || s.Accounts[1].Name != "b" {
		t.Fatal("snapshot not sorted by name", s)
	}
	s.Accounts[0].Value = 100
	if l.Snapshot().Accounts[0].Value != 2 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestDeleteMissingAndReAdd(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Delete, Name: "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	x, _ := l.Apply(Batch{Ops: []Op{
		{Kind: Set, Name: "a", Value: 1},
		{Kind: Delete, Name: "a"},
		{Kind: Add, Name: "a", Delta: 3},
	}})
	if len(x.Changed) != 1 || x.Changed[0].Value != 3 || x.Changed[0].Revision != 2 {
		t.Fatalf("%+v", x)
	}
}

func TestConcurrentStress(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 16, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			name := fmt.Sprintf("acct-%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Kind: Add, Name: name, Delta: 1}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 32 || s.Generation != 32*50 {
		t.Fatal(len(s.Accounts), s.Generation)
	}
	for _, a := range s.Accounts {
		if a.Value != 50 {
			t.Fatal(a)
		}
	}
}
