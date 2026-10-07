package balanceledger332

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, 1, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 16, MaxNameBytes: 8, MaxAbsValue: 20})
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "0", "-", "_", "a-b_c9", "12345678"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndFullValidation(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation of the whole batch happens before state changes:
	// a valid first op must not be applied when a later op is invalid.
	_, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Kind: 0, Name: "b"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(l.Snapshot().Accounts) != 0 {
		t.Fatal("state changed despite invalid batch")
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	// Overflow must be detected before arithmetic, not wrap around.
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// math.MinInt64 has no representable absolute value and must be rejected.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 0}, {Add, "b", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if v := l.Snapshot().Accounts[0].Value; v != math.MaxInt64 {
		t.Fatal(v)
	}

	l2, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, -10}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestRevisionSequenceAndGeneration(t *testing.T) {
	l := led(t)
	r1, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Add, "a", 1, 0}}})
	if e != nil || r1.Generation != 1 || r1.Revision != 3 {
		t.Fatal(e, r1)
	}
	// Failed batch consumes neither revisions nor generation.
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r2, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 1}}})
	if e != nil || r2.Generation != 2 || r2.Revision != 4 {
		t.Fatal(e, r2)
	}
	// Empty batch succeeds without bumping generation.
	r3, e := l.Apply(Batch{})
	if e != nil || r3.Generation != 2 || r3.Revision != 4 {
		t.Fatal(e, r3)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 5 {
		t.Fatal(s)
	}
	// Changed holds the final state per account, in first-touch order.
	if len(r1.Changed) != 2 || r1.Changed[0].Name != "a" || r1.Changed[0].Value != 2 ||
		r1.Changed[0].Revision != 3 || r1.Changed[1].Name != "b" {
		t.Fatal(r1.Changed)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	// Peak of 3 accounts mid-batch, but only 2 remain at the end: allowed.
	_, e := l.Apply(Batch{Ops: []Op{
		{Set, "a", 0, 1}, {Set, "b", 0, 1}, {Set, "c", 0, 1}, {Delete, "c", 0, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Exceeding capacity at batch end fails and rolls back fully.
	_, e = l.Apply(Batch{Ops: []Op{{Set, "c", 0, 1}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(l.Snapshot().Accounts) != 2 {
		t.Fatal("rollback failed")
	}
}

func TestDeleteMissingAndDeletedNotChanged(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r.Changed)
	}
	if len(l.Snapshot().Accounts) != 1 {
		t.Fatal(l.Snapshot())
	}
}

func TestTopOrderingAndLimits(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3}, {Set, "d", 0, 9},
	}})
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "d" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(e, top)
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "c" {
		t.Fatal(all)
	}
	zero, _ := l.Top(0)
	if len(zero) != 0 {
		t.Fatal(zero)
	}
	// Snapshot is sorted by name.
	s := l.Snapshot()
	for i := 1; i < len(s.Accounts); i++ {
		if s.Accounts[i-1].Name >= s.Accounts[i].Name {
			t.Fatal(s.Accounts)
		}
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
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatal(got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i%26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Set, k, 0, int64(j)}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if s.Generation == 0 || s.NextRevision != s.Generation+1 {
		t.Fatal(s.Generation, s.NextRevision)
	}
	// Revisions must be unique and contiguous: 1..generation.
	seen := make(map[uint64]bool)
	for _, a := range s.Accounts {
		if a.Revision == 0 || a.Revision > s.Generation || seen[a.Revision] {
			t.Fatal("bad revision", a)
		}
		seen[a.Revision] = true
	}
}
