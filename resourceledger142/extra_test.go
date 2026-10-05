package resourceledger142

import (
	"errors"
	"fmt"
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
	for _, n := range []string{"", "A", "a b", "a.b", "中文", "toolongname"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: n}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "ok_nm-1", Value: 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Kind(99), Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Add, Name: "a", Delta: 1}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "b", Value: math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Add, Name: "b", Delta: -1}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed batches roll back revisions too.
	s := l.Snapshot()
	if s.NextRevision != 3 || len(s.Accounts) != 2 {
		t.Fatal(s)
	}
}

func TestAbsLimitEnforced(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Add, Name: "a", Delta: 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: -20}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 1}}}); e != nil {
		t.Fatal(e)
	}
	r, e = l.Apply(Batch{})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 1}}}); e != nil {
		t.Fatal(e)
	}
	b := l.Snapshot()
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "b", Value: 2}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := l.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Accounts) != 1 {
		t.Fatal(got)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Kind: Set, Name: "b", Value: 5},
		{Kind: Set, Name: "a", Value: 5},
		{Kind: Set, Name: "c", Value: 9},
		{Kind: Set, Name: "d", Value: -1},
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
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	top, _ := l.Top(1)
	top[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("returned slices alias internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1000})
	p, _ := NewPolicy(2, []string{"w"})
	c, _ := NewCoordinator(l, p)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("acct-%02d", i)
			for j := 0; j < 10; j++ {
				_, _ = c.Apply("w", Batch{Ops: []Op{{Kind: Add, Name: name, Delta: 1}}})
				_, _ = c.Apply("x", Batch{Ops: []Op{{Kind: Add, Name: name, Delta: 1}}})
				_ = l.Snapshot()
				_, _ = l.Top(4)
			}
		}()
	}
	wg.Wait()
	snap := l.Snapshot()
	if len(snap.Accounts) != 32 {
		t.Fatal(len(snap.Accounts))
	}
	for _, a := range snap.Accounts {
		if a.Value != 10 {
			t.Fatal(a)
		}
	}
	// Audit sequences are consecutive and every attempt is logged.
	ds := c.Decisions()
	if len(ds) != 32*10*2 {
		t.Fatal(len(ds))
	}
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal(i, d)
		}
	}
}

func TestPolicyReplaceConcurrent(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	p, _ := NewPolicy(1, []string{"a"})
	c, _ := NewCoordinator(l, p)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := fmt.Sprintf("actor%d", i%2)
			_ = p.ReplaceActors([]string{actor})
			_, _ = c.Apply(actor, Batch{Ops: []Op{{Kind: Add, Name: "k", Delta: 1}}})
			_ = c.Decisions()
		}()
	}
	wg.Wait()
}
