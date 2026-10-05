package resourceledger152

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, n := range []string{"", "A", "a b", "a.b", "中文", "toolongname", "a/b"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Add, n, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-0_", "12345678"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	l := led(t)
	for _, op := range []Op{
		{Kind(0), "a", 1, 0}, {Kind(4), "a", 1, 0}, {Kind(255), "a", 0, 0},
		{Add, "a", 1, 1}, {Set, "a", 1, 1}, {Delete, "a", 1, 0}, {Delete, "a", 0, 1},
	} {
		if _, e := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestAbsLimitAndOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 20})
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Overflow must be detected before arithmetic even within the abs limit path.
	big, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := big.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := big.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := big.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}, {Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed batches roll back fully.
	if got := big.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatal(got)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 || len(s.Accounts) != 1 || s.Accounts[0].Revision != 2 {
		t.Fatal(s)
	}
	// Empty batch: no generation bump, no revision consumed.
	r, e = l.Apply(Batch{})
	if e != nil || r.Generation != 1 || l.Snapshot().NextRevision != 3 {
		t.Fatal(r, e)
	}
	// Delete-only batch bumps generation but consumes no revision.
	r, e = l.Apply(Batch{Ops: []Op{{Delete, "b", 0, 0}}})
	if e != nil || r.Generation != 2 || r.Revision != 2 || l.Snapshot().NextRevision != 3 {
		t.Fatal(r, e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
	// Delete-then-create within one batch fits the final capacity.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "c", 0, 3}}}); e != nil {
		t.Fatal(e)
	}
}

func TestTopOrderAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3}, {Set, "d", 0, 9}}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "d" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatal(all)
	}
	// Mutating returned slices must not affect internal state.
	top[0].Value = -999
	s := l.Snapshot()
	if s.Accounts[0].Name != "a" || s.Accounts[3].Name != "d" || s.Accounts[3].Value != 9 {
		t.Fatal(s)
	}
	s.Accounts[0].Value = 777
	if again := l.Snapshot(); again.Accounts[0].Value != 5 {
		t.Fatal(again)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}, {Add, k, -1, 0}, {Add, k, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 16 {
		t.Fatal(len(s.Accounts))
	}
	for _, a := range s.Accounts {
		if a.Value != 50 {
			t.Fatal(a)
		}
	}
	// Revisions are contiguous: 16 keys * 50 rounds * 3 ops.
	if s.NextRevision != 16*50*3+1 {
		t.Fatal(s.NextRevision)
	}
	if s.Generation != 16*50 {
		t.Fatal(s.Generation)
	}
}
