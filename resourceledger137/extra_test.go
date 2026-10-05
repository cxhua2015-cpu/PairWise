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
	for _, name := range []string{"", "A", "a b", "a.b", "中文", "toolongname"} {
		if _, err := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, err)
		}
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "ok_nm-1", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Kind(99), "a", 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	small, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 5})
	if _, err := small.Apply(Batch{Ops: []Op{{Set, "a", 0, 6}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := small.Apply(Batch{Ops: []Op{{Add, "a", -6, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	r, err = l.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
	if got := l.Snapshot().Generation; got != 1 {
		t.Fatal(got)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}}})
	top, err := l.Top(2)
	if err != nil || len(top) != 2 || top[0].Name != "c" || top[1].Name != "a" {
		t.Fatal(top, err)
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	top[0].Value = -999
	if l.Snapshot().Accounts[2].Value != 9 {
		t.Fatal("returned slice aliases state")
	}
}

func TestRevisionSequence(t *testing.T) {
	l := led(t)
	r, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "a", 0, 2}, {Delete, "a", 0, 0}}})
	if r.Revision != 2 || len(r.Changed) != 0 {
		t.Fatal(r)
	}
	if s := l.Snapshot(); s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestPolicyAndCoordinatorEdges(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	p, err := NewPolicy(2, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 3); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("b", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
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

func TestCoordinatorEngineFailureAudited(t *testing.T) {
	core, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	p, _ := NewPolicy(4, []string{"a"})
	c, _ := NewCoordinator(core, p)
	if _, err := c.Apply("a", Batch{Ops: []Op{{Delete, "ghost", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	d := c.Decisions()
	if len(d) != 1 || d[0].Committed || d[0].Error == "" || d[0].Sequence != 1 {
		t.Fatal(d)
	}
}

func TestConcurrentCoordinatorAndPolicy(t *testing.T) {
	core, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 1000})
	p, _ := NewPolicy(2, []string{"alice", "bob"})
	c, _ := NewCoordinator(core, p)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := "alice"
			if i%2 == 0 {
				actor = "bob"
			}
			_, _ = c.Apply(actor, Batch{Ops: []Op{{Add, "acct", 1, 0}}})
			_ = c.Decisions()
			if i%3 == 0 {
				_ = p.ReplaceActors([]string{"alice", "bob"})
			}
		}()
	}
	wg.Wait()
	decisions := c.Decisions()
	if len(decisions) != 16 {
		t.Fatal(len(decisions))
	}
	for i, d := range decisions {
		if d.Sequence != uint64(i+1) {
			t.Fatal("non-contiguous sequence", d)
		}
	}
	if got := core.Snapshot().Accounts[0].Value; got != 16 {
		t.Fatal(got)
	}
}
