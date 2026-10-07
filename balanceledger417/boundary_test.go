package balanceledger417

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("overflow: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("minint abs: %v", err)
	}
	if got := l.Snapshot().Accounts; len(got) != 1 || got[0].Value != math.MaxInt64 {
		t.Fatalf("rollback: %+v", got)
	}
}

func TestValidationPreflight(t *testing.T) {
	l := led(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Add, "", 1, 0}}},
		{Ops: []Op{{Add, "Upper", 1, 0}}},
		{Ops: []Op{{Add, "toolongname", 1, 0}}},
		{Ops: []Op{{Add, "a b", 1, 0}}},
		{Ops: []Op{{Add, "a", 0, 0}}},
	}
	for i, b := range bad {
		if err := l.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := l.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Set, "ok-1_", 0, 5}}}); err != nil {
		t.Fatal(err)
	}
	if s := l.Stats(); s.Accounts != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatalf("empty: %+v %v", r, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 2}, {Set, "b", 0, 3}}}); err != nil {
		t.Fatal(err)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatalf("clocks: %+v", s)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}}})
	top, err := l.Top(2)
	if err != nil || len(top) != 2 || top[0].Name != "c" || top[1].Name != "a" {
		t.Fatalf("top: %+v %v", top, err)
	}
	if _, err := l.Top(0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("top(0): %v", err)
	}
	top[0].Value = -999
	if l.Snapshot().Accounts[2].Value != 9 {
		t.Fatal("returned slice aliases state")
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 7}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 0 {
		t.Fatal("clone shares ownership with original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_ = l.Stats()
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
			}
		}()
	}
	w.Wait()
	s := l.Stats()
	if s.Accounts != 32 || s.NextRevision != 32*50+1 || s.Generation != 32*50 {
		t.Fatalf("stats: %+v", s)
	}
	c, err := l.Clone()
	if err != nil || c.Stats() != s {
		t.Fatalf("clone: %+v %v", c.Stats(), err)
	}
}
