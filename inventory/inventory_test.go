package inventory

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
	for _, n := range []string{"", "A", "a b", "a.b", "中文", "toolongname", "a/b"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-0_", "12345678"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndStructuralFirst(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(9), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural error later in batch must prevent earlier valid ops.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Kind(0), "b", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(l.Snapshot().Accounts) != 0 {
		t.Fatal("state mutated by invalid batch")
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}, {Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != math.MaxInt64 {
		t.Fatal(s)
	}

	l2 := led(t) // MaxAbsValue 20
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "a", -21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if len(l2.Snapshot().Accounts) != 0 {
		t.Fatal("failed batch leaked state")
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t) // MaxAccounts 4
	ops := []Op{{Set, "a", 0, 1}, {Set, "b", 0, 1}, {Set, "c", 0, 1}, {Set, "d", 0, 1}}
	if _, e := l.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	g := l.Snapshot().Generation
	// Net +1 account at batch end must fail and roll back entirely.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "e", 0, 1}, {Set, "f", 0, 1}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != g || len(s.Accounts) != 4 {
		t.Fatal(s)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{}})
	if r.Generation != 1 || l.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestRevisionAndChanged(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if e != nil || r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r, e)
	}
	s := l.Snapshot()
	if s.NextRevision != 3 || s.Accounts[0].Revision != 2 {
		t.Fatal(s)
	}
}

func TestTopAndSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 5}, {Set, "b", 0, 5}, {Set, "c", 0, 9}}})
	top, e := l.Top(10)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	top[0].Value = -100
	s := l.Snapshot()
	if s.Accounts[2].Value != 9 {
		t.Fatal("Top result aliases internal state")
	}
	s.Accounts[0].Value = -100
	if l.Snapshot().Accounts[0].Value != 5 {
		t.Fatal("Snapshot aliases internal state")
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestConcurrentSum(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100000})
	const workers, per = 16, 250
	var w sync.WaitGroup
	for i := 0; i < workers; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < per; j++ {
				if _, e := l.Apply(Batch{Ops: []Op{{Add, "ctr", 1, 0}}}); e != nil {
					t.Error(e)
					return
				}
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if s.Accounts[0].Value != workers*per {
		t.Fatal(s.Accounts[0].Value)
	}
	if s.Generation != workers*per || s.NextRevision != workers*per+1 {
		t.Fatal(s.Generation, s.NextRevision)
	}
}
