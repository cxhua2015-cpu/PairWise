package balanceledger222

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Add, "", 1, 0}}},
		{Ops: []Op{{Add, "A", 1, 0}}},
		{Ops: []Op{{Add, "a b", 1, 0}}},
		{Ops: []Op{{Add, "toolongname", 1, 0}}},
		{Ops: []Op{{Add, "a", 0, 0}}},
		{Ops: []Op{{Add, "a", 1, 1}}},
		{Ops: []Op{{Set, "a", 1, 0}}},
		{Ops: []Op{{Delete, "a", 1, 0}}},
		{Ops: []Op{{Delete, "a", 0, 1}}},
	}
	for i, b := range bad {
		if e := l.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if e := l.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// ValidateBatch must not mutate state.
	if s := l.Snapshot(); s.Generation != 0 || len(s.Accounts) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Add, "ok-name1", 5, 0}, {Set, "b", 0, -20}, {Delete, "c", 0, 0}}}); e != nil {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	r, e = l.Apply(Batch{})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
	if l.Stats().Generation != 1 {
		t.Fatal(l.Stats())
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	// Net-zero intermediate growth still fails at batch end.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Delete, "c", 0, 0}, {Set, "d", 0, 4}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 2 || s.Generation != 1 || s.NextRevision != 3 {
		t.Fatalf("state changed after rollback: %+v", s)
	}
	// Delete-then-create within capacity succeeds.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "c", 0, 3}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionSequence(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Add, "b", 1, 0}}})
	if e != nil || r.Revision != 3 {
		t.Fatal(r, e)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Changed[0].Revision != 3 {
		t.Fatal(r.Changed)
	}
	if l.Stats().NextRevision != 4 {
		t.Fatal(l.Stats())
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3}, {Set, "d", 0, 7}}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	want := []string{"d", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	top[0].Value = -999
	if l.Snapshot().Accounts[3].Value != 7 {
		t.Fatal("returned slice aliases internal state")
	}
	snap := l.Snapshot()
	names := []string{"a", "b", "c", "d"}
	for i, n := range names {
		if snap.Accounts[i].Name != n {
			t.Fatal(snap.Accounts)
		}
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 5}}})
	c, e := l.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone lost logical clocks")
	}
	if _, e = c.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	if l.Stats().Accounts != 1 || l.Snapshot().Accounts[0].Value != 5 {
		t.Fatal("clone write leaked into original")
	}
	if c.Stats().Accounts != 2 {
		t.Fatal("clone did not evolve independently")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := l.Stats()
	if s.Accounts != 32 || s.NextRevision != 32*50+1 || s.Generation != 32*50 {
		t.Fatalf("inconsistent final state: %+v", s)
	}
}
