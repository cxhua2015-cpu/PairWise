package resourceledger147

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {4, 8, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	for _, b := range []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Add, "", 1, 0}}},
		{Ops: []Op{{Add, "Upper", 1, 0}}},
		{Ops: []Op{{Add, "a b", 1, 0}}},
		{Ops: []Op{{Add, "toolongname", 1, 0}}},
	} {
		if _, e := l.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(b, e)
		}
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	small, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 5})
	if _, e := small.Apply(Batch{Ops: []Op{{Set, "a", 0, 6}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := small.Apply(Batch{Ops: []Op{{Set, "a", 0, -6}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	r, e = l.Apply(Batch{})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 2 {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	before := l.Snapshot()
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := l.Snapshot(); got.Generation != before.Generation || len(got.Accounts) != 2 {
		t.Fatal(got)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1}}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(all)
	}
	top[0].Name = "mutated"
	again, _ := l.Top(1)
	if again[0].Name != "c" {
		t.Fatal("Top aliases internal state")
	}
}

func TestRevisionSequence(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "a", 0, 2}, {Delete, "a", 0, 0}, {Set, "b", 0, 1}}})
	if e != nil || r.Revision != 3 {
		t.Fatal(r, e)
	}
	accs := l.Snapshot().Accounts
	if len(accs) != 1 || accs[0].Name != "b" || accs[0].Revision != 3 {
		t.Fatal(accs)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000000})
	p, _ := NewPolicy(4, []string{"w"})
	c, _ := NewCoordinator(l, p)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for j := 0; j < 50; j++ {
				_, _ = c.Apply("w", Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = c.Decisions()
			}
		}()
	}
	wg.Wait()
	if e := p.ReplaceActors([]string{"x"}); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Apply("w", Batch{Ops: []Op{{Add, "a0", 1, 0}}}); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	ds := c.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("non-contiguous sequence", d)
		}
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, e := NewPolicy(0, []string{"a"}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := NewPolicy(1, []string{""}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	p, _ := NewPolicy(2, []string{"a"})
	if e := p.Authorize("a", 3); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.Authorize("b", 1); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.ReplaceActors(nil); e != nil {
		t.Fatal(e)
	}
	if e := p.Authorize("a", 1); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if _, e := NewCoordinator(nil, p); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}
