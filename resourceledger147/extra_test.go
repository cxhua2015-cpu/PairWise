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

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, n := range []string{"", "A", "a b", "a/b", "toolongname", "é"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(n, e)
		}
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatal(got)
	}
	l2 := led(t)
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || l.Snapshot().Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}})
	if r.Generation != 1 || r.Revision != 2 || l.Snapshot().NextRevision != 3 {
		t.Fatal(r)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}}})
	top, e := l.Top(2)
	if e != nil || len(top) != 2 || top[0].Name != "c" || top[1].Name != "a" {
		t.Fatal(top, e)
	}
	top[0].Name = "mut"
	if l.Top(1); true {
		again, _ := l.Top(1)
		if again[0].Name != "c" {
			t.Fatal("top aliases state")
		}
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	snap := l.Snapshot()
	if snap.Accounts[0].Name != "a" || snap.Accounts[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Accounts[0].Value = -99
	if l.Snapshot().Accounts[0].Value != 5 {
		t.Fatal("snapshot aliases state")
	}
}

func TestConcurrentCoordinator(t *testing.T) {
	core, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 16, MaxAbsValue: 1000000})
	pol, _ := NewPolicy(2, []string{"alice", "bob"})
	coord, _ := NewCoordinator(core, pol)
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
			if i%5 == 0 {
				_ = pol.ReplaceActors([]string{"alice", "bob"})
			}
			_, _ = coord.Apply(actor, Batch{Ops: []Op{{Add, "acct", 1, 0}}})
			_ = coord.Decisions()
		}()
	}
	w.Wait()
	ds := coord.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("non-contiguous sequence", ds)
		}
	}
	v := core.Snapshot().Accounts[0].Value
	var committed int64
	for _, d := range ds {
		if d.Committed {
			committed++
		}
	}
	if v != committed {
		t.Fatal(v, committed)
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, e := NewPolicy(0, nil); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	p, _ := NewPolicy(1, []string{"a"})
	if e := p.ReplaceActors([]string{""}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := p.Authorize("a", 2); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.Authorize("a", 1); e != nil {
		t.Fatal(e)
	}
	if _, e := NewCoordinator(nil, p); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
}
