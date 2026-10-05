package resourceledger132

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
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	l := led(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Add, "", 1, 0}}},
		{Ops: []Op{{Add, "Upper", 1, 0}}},
		{Ops: []Op{{Add, "a b", 1, 0}}},
		{Ops: []Op{{Add, "toolongname", 1, 0}}},
		{Ops: []Op{{Add, "中文", 1, 0}}},
	}
	for _, b := range bad {
		if _, e := l.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(b, e)
		}
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal("invalid batch mutated generation", g)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Accounts[0].Value != math.MinInt64+1 || s.Generation != 2 {
		t.Fatal(s)
	}
}

func TestAbsLimitAndCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 5})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 6}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -6}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal("capacity failure must roll back", n)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	r, e = l.Apply(Batch{})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
	if s := l.Snapshot(); s.NextRevision != 2 || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestRevisionSequenceAndChanged(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{
		{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Add, "a", 4, 0},
	}})
	if e != nil || r.Revision != 3 {
		t.Fatal(r, e)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[0].Value != 4 || r.Changed[0].Revision != 3 {
		t.Fatal(r.Changed)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if s := l.Snapshot(); s.NextRevision != 4 {
		t.Fatal("delete must not consume revision", s)
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
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r.Changed[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 99
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("returned slices alias internal state")
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, e := NewPolicy(0, []string{"a"}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := NewPolicy(1, []string{""}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	p, _ := NewPolicy(2, []string{"a"})
	if e := p.Authorize("b", 1); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.Authorize("a", 3); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.Authorize("a", 2); e != nil {
		t.Fatal(e)
	}
	if e := p.ReplaceActors(nil); e != nil {
		t.Fatal(e)
	}
	if e := p.Authorize("a", 1); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
}

func TestCoordinatorNilAndEngineFailure(t *testing.T) {
	if _, e := NewCoordinator(nil, nil); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	l := led(t)
	p, _ := NewPolicy(4, []string{"a"})
	c, _ := NewCoordinator(l, p)
	if _, e := c.Apply("a", Batch{Ops: []Op{{Delete, "ghost", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	d := c.Decisions()
	if len(d) != 1 || d[0].Sequence != 1 || d[0].Committed || d[0].Error == "" {
		t.Fatal(d)
	}
	if _, e := c.Apply("a", Batch{Ops: []Op{{Add, "x", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	d = c.Decisions()
	if len(d) != 2 || d[1].Sequence != 2 || !d[1].Committed || d[1].Generation != 1 || d[1].Error != "" {
		t.Fatal(d)
	}
}

func TestConcurrentAllLayers(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 16, MaxAbsValue: 1000000})
	p, _ := NewPolicy(2, []string{"alice", "bob"})
	c, _ := NewCoordinator(l, p)
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			actor := "alice"
			if i%3 == 0 {
				actor = "mallory"
			}
			name := string(rune('a'+i%26)) + "-" + string(rune('a'+(i/26)%26))
			_, _ = c.Apply(actor, Batch{Ops: []Op{{Add, name, 1, 0}}})
			_, _ = l.Top(4)
			_ = l.Snapshot()
			_ = c.Decisions()
			if i%5 == 0 {
				_ = p.ReplaceActors([]string{"alice", "bob"})
			}
		}()
	}
	w.Wait()
	ds := c.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("audit sequence gap", d)
		}
	}
	var committed int
	for _, d := range ds {
		if d.Committed {
			committed++
		}
	}
	if int(l.Snapshot().Generation) != committed {
		t.Fatal("committed decisions and generations diverge", committed, l.Snapshot().Generation)
	}
}
