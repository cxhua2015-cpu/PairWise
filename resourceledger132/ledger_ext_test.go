package resourceledger132

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
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, name := range []string{"", "A", "a b", "a/b", "工具", "toolongname"} {
		if _, err := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, err)
		}
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Kind(99), "a", 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "z-0_9", "12345678"} {
		if _, err := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); err != nil {
			t.Fatalf("name %q: %v", name, err)
		}
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
	if _, err := small.Apply(Batch{Ops: []Op{{Add, "a", 6, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if got := small.Snapshot().Generation; got != 0 {
		t.Fatal("failed batches must not bump generation", got)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal("capacity failure must roll back", n)
	}
	// Transient overflow within batch is fine: capacity checked only at end.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}, {Delete, "c", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchAndRevision(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Add, "a", 1, 0}}})
	if r.Generation != 1 || r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Revision != 2 {
		t.Fatal(r)
	}
	s := l.Snapshot()
	if s.NextRevision != 3 || s.Generation != 1 {
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
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "d" {
		t.Fatal(all)
	}
}

func TestConcurrentMixed(t *testing.T) {
	core, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 16, MaxAbsValue: 1000})
	policy, _ := NewPolicy(2, []string{"alice", "bob"})
	coord, _ := NewCoordinator(core, policy)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := "alice"
			if i%3 == 0 {
				actor = "mallory"
			}
			_, _ = coord.Apply(actor, Batch{Ops: []Op{{Add, fmt.Sprintf("acct-%d", i%8), 1, 0}}})
			_, _ = core.Top(4)
			_ = core.Snapshot()
			_ = coord.Decisions()
			if i%5 == 0 {
				_ = policy.ReplaceActors([]string{"alice", "bob"})
			}
		}()
	}
	wg.Wait()
	ds := coord.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("audit sequence not contiguous", i, d)
		}
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(1, []string{"bad actor"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, _ := NewPolicy(1, []string{"a"})
	if err := p.ReplaceActors([]string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Failed replacement must keep the old list.
	if err := p.Authorize("a", 1); err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 2); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if _, err := NewCoordinator(nil, p); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
}
