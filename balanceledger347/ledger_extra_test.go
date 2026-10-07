package balanceledger347

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "a.b", "é", "toolongname", "a+b"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	l2, _ := New(Options{MaxAccounts: 16, MaxNameBytes: 8, MaxAbsValue: 20})
	for _, n := range []string{"a", "0", "-", "_", "a-b_c0", "12345678"} {
		if _, e := l2.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r, _ = l.Apply(Batch{Ops: nil})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	if g := l.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestAbsLimitOnInput(t *testing.T) {
	l := led(t) // MaxAbsValue = 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Set, "b", 0, -20}}}); e != nil {
		t.Fatal(e)
	}
}

func TestOverflowDetection(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}, {Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed batches must not change state.
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != math.MaxInt64 || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	_, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}, {Set, "c", 0, 3}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 1 {
		t.Fatal(n)
	}
	// Delete-then-create within one batch only checks the final count.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionSequence(t *testing.T) {
	l := led(t)
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Add, "b", 2, 0}}})
	if r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Changed[0].Revision != 2 {
		t.Fatal(r)
	}
	s := l.Snapshot()
	if s.NextRevision != 3 || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestTopOrderAndIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 5}, {Set, "b", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1}}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	top[0].Value = -100
	if l.Snapshot().Accounts[2].Value != 9 {
		t.Fatal("returned slice aliases internal state")
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(len(all))
	}
	snap := l.Snapshot()
	for i := 1; i < len(snap.Accounts); i++ {
		if snap.Accounts[i-1].Name >= snap.Accounts[i].Name {
			t.Fatal("snapshot not sorted by name")
		}
	}
	snap.Accounts[0].Value = 12345
	if l.Snapshot().Accounts[0].Value == 12345 {
		t.Fatal("snapshot aliases internal state")
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
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}, {Add, k, -1, 0}, {Set, k, 0, int64(j % 10)}}})
				_, _ = l.Top(8)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 16 || s.Generation != 16*50 {
		t.Fatal(len(s.Accounts), s.Generation)
	}
	if s.NextRevision != 1+16*50*3 {
		t.Fatal(s.NextRevision)
	}
}
