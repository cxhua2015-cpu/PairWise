package balanceledger372

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

func TestInvalidInput(t *testing.T) {
	l := led(t)
	bad := []Op{
		{Kind(0), "a", 0, 0},
		{Kind(9), "a", 0, 0},
		{Add, "", 1, 0},
		{Add, "A", 1, 0},
		{Add, "a b", 1, 0},
		{Add, "toolongname", 1, 0},
		{Add, "中文", 1, 0},
	}
	for _, op := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	// Structural validation of the whole batch happens before any state change.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "ok", 0, 1}, {Kind(0), "x", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(l.Snapshot().Accounts) != 0 {
		t.Fatal("state mutated by invalid batch")
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -5}, {Add, "b", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != math.MaxInt64 {
		t.Fatal(s)
	}

	l2, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "x", 0, 11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "x", 0, -10}, {Add, "x", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	l := led(t)
	r0, e := l.Apply(Batch{})
	if e != nil || r0.Generation != 0 || l.Snapshot().Generation != 0 {
		t.Fatal(r0, e)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	if len(r1.Changed) != 1 || r1.Changed[0].Name != "b" || r1.Changed[0].Revision != 2 {
		t.Fatal(r1.Changed)
	}
	// Failed batch must not bump generation or consume revisions.
	_, _ = l.Apply(Batch{Ops: []Op{{Delete, "nope", 0, 0}}})
	r2, _ := l.Apply(Batch{Ops: []Op{{Add, "b", 1, 0}}})
	if r2.Generation != 2 || r2.Revision != 3 || l.Snapshot().NextRevision != 4 {
		t.Fatal(r2, l.Snapshot())
	}
}

func TestTopAndSnapshotOrder(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "c", 0, 5}, {Set, "a", 0, 5}, {Set, "b", 0, 9}, {Set, "d", 0, -1},
	}})
	top, _ := l.Top(3)
	want := []string{"b", "a", "c"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	full, _ := l.Top(100)
	if len(full) != 4 || full[3].Name != "d" {
		t.Fatal(full)
	}
	snap := l.Snapshot()
	names := []string{"a", "b", "c", "d"}
	for i, a := range snap.Accounts {
		if a.Name != names[i] {
			t.Fatal(snap.Accounts)
		}
	}
	// Returned slices are isolated from internal state.
	top[0].Value = -999
	snap.Accounts[0].Value = -999
	again, _ := l.Top(1)
	if again[0].Value != 9 {
		t.Fatal(again)
	}
}

func TestConcurrentStress(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i%26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	var sum int64
	for _, a := range l.Snapshot().Accounts {
		sum += a.Value
	}
	if sum <= 0 {
		t.Fatal(sum)
	}
}
