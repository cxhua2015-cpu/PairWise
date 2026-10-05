package resourceledger137

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
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, name := range []string{"", "A", "a b", "a.b", "toolongname", "é"} {
		if _, err := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, err)
		}
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "ok-nam_1", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Kind(0), "a", 0, 1}, {Set, "b", 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if got := len(l.Snapshot().Accounts); got != 1 {
		t.Fatal(got)
	}
}

func TestOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
}

func TestAbsLimitAndRevision(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	r, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Set, "b", 0, -20}}})
	if err != nil || r.Revision != 2 || r.Generation != 1 {
		t.Fatal(r, err)
	}
	s := l.Snapshot()
	if s.NextRevision != 3 || s.Accounts[0].Revision != 1 || s.Accounts[1].Revision != 2 {
		t.Fatal(s)
	}
	// Empty batch must not bump generation.
	r, err = l.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Name != "a" || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, err)
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	top, _ = l.Top(100)
	if len(top) != 4 || top[3].Name != "d" {
		t.Fatal(top)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	p, _ := NewPolicy(4, []string{"w"})
	c, _ := NewCoordinator(l, p)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a'+i%8)) + "x"
			for j := 0; j < 50; j++ {
				_, _ = c.Apply("w", Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = c.Decisions()
			}
			if i == 0 {
				_ = p.ReplaceActors([]string{"w", "v"})
			}
		}()
	}
	wg.Wait()
	var total int64
	for _, a := range l.Snapshot().Accounts {
		total += a.Value
	}
	if total != 16*50 {
		t.Fatal(total)
	}
	d := c.Decisions()
	for i, dec := range d {
		if dec.Sequence != uint64(i+1) || !dec.Committed {
			t.Fatal(dec)
		}
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(1, []string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, _ := NewPolicy(2, []string{"a"})
	if err := p.ReplaceActors(nil); err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if _, err := NewCoordinator(nil, p); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
}

func TestCoordinatorEngineFailure(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	p, _ := NewPolicy(4, []string{"a"})
	c, _ := NewCoordinator(l, p)
	if _, err := c.Apply("a", Batch{Ops: []Op{{Delete, "ghost", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	d := c.Decisions()
	if len(d) != 1 || d[0].Committed || d[0].Error == "" || d[0].Sequence != 1 {
		t.Fatal(d)
	}
}
