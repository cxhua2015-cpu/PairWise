package costledger

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
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "toolongname", "中文", "a.b"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "ok-nm_1", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	cases := []Op{
		{Kind(0), "a", 0, 0},
		{Kind(99), "a", 0, 0},
		{Add, "a", 1, 1},    // Add with extra Value
		{Set, "a", 1, 1},    // Set with extra Delta
		{Delete, "a", 1, 0}, // Delete with extra Delta
		{Delete, "a", 0, 1}, // Delete with extra Value
	}
	for _, op := range cases {
		if _, e := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: want ErrInvalidInput, got %v", op, e)
		}
	}
	// Structural check of the whole batch happens before state reads:
	// a valid delete of a missing account plus an invalid op must fail
	// with ErrInvalidInput, not ErrNotFound.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}, {Kind(7), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("want ErrValue on overflow, got %v", e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("want ErrValue on MinInt64 abs, got %v", e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "c", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("want ErrValue on negative overflow, got %v", e)
	}
	// Failed batches must not consume revisions.
	s := l.Snapshot()
	if s.Accounts[0].Value != math.MaxInt64 {
		t.Fatal(s)
	}
	l2 := led(t) // MaxAbsValue 20
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", -41, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	// Failed batch must not bump generation.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if g := l.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
	// In-batch delete frees capacity for the final check.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}, {Delete, "c", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "a", 0, 5}, {Set, "b", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -3},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	want := []string{"c", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if got, _ := l.Top(0); len(got) != 0 {
		t.Fatal(got)
	}
	if got, _ := l.Top(100); len(got) != 4 {
		t.Fatal(got)
	}
	// Mutating returned slices must not affect internal state.
	top[0].Value = -99
	snap := l.Snapshot()
	snap.Accounts[0].Value = -99
	again, _ := l.Top(4)
	if again[0].Name != "c" || again[0].Value != 9 {
		t.Fatal(again)
	}
	if s := l.Snapshot(); s.NextRevision != 5 || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestChangedOrderAndRevision(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{
		{Add, "x", 1, 0}, {Add, "y", 1, 0}, {Add, "x", 1, 0}, {Delete, "y", 0, 0},
	}})
	if e != nil || r.Revision != 3 {
		t.Fatal(r, e)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "x" || r.Changed[0].Revision != 3 || r.Changed[0].Value != 2 {
		t.Fatal(r.Changed)
	}
	if r.Changed[1].Name != "y" {
		t.Fatal(r.Changed)
	}
	if n := len(l.Snapshot().Accounts); n != 1 {
		t.Fatal(n)
	}
}

func TestConcurrentStress(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	const workers = 16
	var w sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			name := fmt.Sprintf("acct-%02d", i)
			for j := 0; j < 50; j++ {
				if _, e := l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}}); e != nil {
					t.Error(e)
					return
				}
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != workers || s.Generation != workers*50 {
		t.Fatal(len(s.Accounts), s.Generation)
	}
	for _, a := range s.Accounts {
		if a.Value != 50 {
			t.Fatal(a)
		}
	}
	// Revisions are contiguous: 1..workers*50.
	if s.NextRevision != workers*50+1 {
		t.Fatal(s.NextRevision)
	}
}
