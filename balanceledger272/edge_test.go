package balanceledger272

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {-1, 8, 10}, {4, 8, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	bad := []Op{
		{Kind: 0, Name: "a"},
		{Kind: 99, Name: "a"},
		{Add, "", 1, 0},
		{Add, "A", 1, 0},
		{Add, "a b", 1, 0},
		{Add, "toolongname", 1, 0},
		{Add, "a", 0, 0},    // zero delta
		{Add, "a", 1, 1},    // extra field
		{Set, "a", 1, 1},    // extra field
		{Delete, "a", 1, 0}, // extra field
	}
	for _, op := range bad {
		if e := l.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	good := []Op{{Add, "a-1_b", 1, 0}, {Set, "a", 0, -3}, {Delete, "a", 0, 0}}
	for _, op := range good {
		if e := l.ValidateBatch(Batch{Ops: []Op{op}}); e != nil {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	// Validation must not mutate state.
	if s := l.Snapshot(); s.Generation != 0 || len(s.Accounts) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
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
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != math.MaxInt64 {
		t.Fatalf("state corrupted: %+v", s)
	}

	small, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 5})
	if _, e := small.Apply(Batch{Ops: []Op{{Set, "a", 0, 6}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := small.Apply(Batch{Ops: []Op{{Set, "a", 0, -5}}}); e != nil {
		t.Fatal(e)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if r.Generation != 2 || r.Revision != 1 || len(r.Changed) != 1 {
		t.Fatal(r)
	}
	if z := l.Stats(); z.Generation != 2 || z.NextRevision != 2 || z.Accounts != 0 {
		t.Fatalf("stats: %+v", z)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal("capacity failure must roll back", n)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 99
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatal("returned slices alias internal state", got)
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	c, e := l.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "b", 0, 7}}})
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 1 {
		t.Fatal("clone shares ownership with original")
	}
	if l.Snapshot().Accounts[0].Name != "a" || c.Snapshot().Accounts[0].Name != "b" {
		t.Fatal("clone mutation leaked")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	z := l.Stats()
	if z.Accounts != 32 || z.Generation != 32*50 {
		t.Fatalf("stats: %+v", z)
	}
	var total int64
	for _, a := range l.Snapshot().Accounts {
		total += a.Value
	}
	if total != 32*50 {
		t.Fatal("lost updates", total)
	}
}
