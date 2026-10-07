package balanceledger402

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
	for _, name := range []string{"", "A", "a b", "a/b", "é", "toolongname"} {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, name, 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, err)
		}
	}
	for _, name := range []string{"a", "z-0_9", "abcdefgh"} {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, name, 0, 1}}}); err != nil {
			t.Fatalf("name %q: %v", name, err)
		}
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	l := led(t)
	for _, op := range []Op{
		{Kind(0), "a", 1, 0}, {Kind(9), "a", 1, 0},
		{Add, "a", 0, 0}, {Add, "a", 1, 1},
		{Set, "a", 1, 1}, {Delete, "a", 1, 0}, {Delete, "a", 0, 1},
	} {
		if err := l.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("overflow: %v", err)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatalf("state mutated after overflow: %d", got)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("min int64 abs: %v", err)
	}
	small, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 5})
	if _, err := small.Apply(Batch{Ops: []Op{{Set, "a", 0, 6}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("set beyond abs limit: %v", err)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	if got := l.Snapshot(); got.Generation != before.Generation || len(got.Accounts) != 1 {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	if _, err := l.Apply(Batch{}); err != nil {
		t.Fatal(err)
	}
	if g := l.Stats().Generation; g != 0 {
		t.Fatalf("empty batch changed generation: %d", g)
	}
	r, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}})
	if err != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatalf("result %+v err %v", r, err)
	}
	if s := l.Stats(); s.NextRevision != 3 || s.Accounts != 2 {
		t.Fatalf("stats %+v", s)
	}
}

func TestTopOrdering(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -1}}})
	top, err := l.Top(10)
	if err != nil || len(top) != 3 || top[0].Name != "a" || top[1].Name != "b" || top[2].Name != "c" {
		t.Fatalf("top %+v err %v", top, err)
	}
	if _, err := l.Top(0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("top 0: %v", err)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	snap := l.Snapshot()
	snap.Accounts[0].Value = 99
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatalf("snapshot aliases state: %d", got)
	}
}

func TestCloneIndependentClocks(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone lost logical clocks")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 0 {
		t.Fatal("clone shares state with original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_ = l.Stats()
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_, _ = l.Clone()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Add, k, 1, 0}}})
			}
		}()
	}
	w.Wait()
	s := l.Stats()
	if s.Accounts != 16 || s.Generation != 800 || s.NextRevision != 801 {
		t.Fatalf("stats %+v", s)
	}
}
