package resourceledger187

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
			t.Fatal(o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, n := range []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-9_", "ab-c_d12"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(0), "a", 0, 0}, {Delete, "ghost", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("unknown kind must fail structural validation before state reads", e)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -5}, {Add, "b", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != math.MaxInt64 {
		t.Fatal("failed batches must roll back", s)
	}
}

func TestAbsLimitAndGeneration(t *testing.T) {
	l := led(t)
	r1, e := l.Apply(Batch{})
	if e != nil || r1.Generation != 0 {
		t.Fatal("empty batch must not bump generation", r1, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}}}); e != nil {
		t.Fatal(e)
	}
	r2, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	if r2.Generation != 2 {
		t.Fatal("generation must bump once per non-empty success", r2)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Add, "a", 100, 0}, {Add, "a", 100, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if l.Snapshot().Generation != 2 {
		t.Fatal("failed batch must not bump generation")
	}
}

func TestRevisionSequence(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if e != nil || x.Revision != 2 {
		t.Fatal(x, e)
	}
	if len(x.Changed) != 1 || x.Changed[0].Name != "b" || x.Changed[0].Revision != 2 {
		t.Fatal(x.Changed)
	}
	s := l.Snapshot()
	if s.NextRevision != 3 || s.Accounts[0].Name != "b" {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 2 {
		t.Fatal("capacity failure must roll back", n)
	}
	// Replace within capacity: delete then create in same batch.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "c", 0, 3}}}); e != nil {
		t.Fatal(e)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3}, {Set, "d", 0, 9},
	}})
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
	if _, e = l.Top(0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(all)
	}
	// Mutating the returned slice must not affect internal state.
	all[0].Value = -999
	again, _ := l.Top(1)
	if again[0].Value != 9 {
		t.Fatal("returned slices must be isolated from internal state")
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
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}, {Add, k, -1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) == 0 {
		t.Fatal("expected accounts")
	}
	for i := 1; i < len(s.Accounts); i++ {
		if s.Accounts[i-1].Name >= s.Accounts[i].Name {
			t.Fatal("snapshot must be sorted by name")
		}
	}
	if s.NextRevision != s.Generation*2+1 {
		t.Fatal("revision/generation accounting drifted", s.Generation, s.NextRevision)
	}
}
