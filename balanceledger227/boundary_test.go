package balanceledger227

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

func TestNameRules(t *testing.T) {
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

func TestUnknownKind(t *testing.T) {
	l := led(t)
	if err := l.ValidateBatch(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 1, 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestAbsLimitAndOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64 - 1})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64 - 2}}}); err != nil {
		t.Fatal(err)
	}
	// MaxInt64-2 + 2 would overflow int64; must be detected before arithmetic.
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 2, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64-2 {
		t.Fatalf("state mutated on overflow: %d", got)
	}
	// -1 - (MaxInt64-1) = -MaxInt64 breaches the absolute-value limit.
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", -(math.MaxInt64 - 1), 0}, {Add, "a", -(math.MaxInt64 - 1), 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	if g := l.Stats().Generation; g != 1 {
		t.Fatalf("generation=%d", g)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	// Intermediate state stays within capacity, final state exceeds it.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := l.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Accounts) != 1 {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestTopOrder(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -3}}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 {
		t.Fatal(top, err)
	}
	want := []string{"c", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatalf("top[%d]=%s want %s", i, top[i].Name, w)
		}
	}
	if _, err := l.Top(0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Returned slice must be isolated from internal state.
	top[0].Value = -999
	if l.Snapshot().Accounts[2].Value != 9 {
		t.Fatal("Top result aliases internal state")
	}
}

func TestValidateBatchIsSideEffectFree(t *testing.T) {
	l := led(t)
	before := l.Snapshot()
	_ = l.ValidateBatch(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	_ = l.ValidateBatch(Batch{Ops: []Op{{Kind(9), "a", 0, 0}}})
	if got := l.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Accounts) != 0 {
		t.Fatal("ValidateBatch mutated state")
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 3}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone does not preserve logical clocks")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "b", 0, 7}}}); err != nil {
		t.Fatal(err)
	}
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 1 {
		t.Fatal("clone shares state with original")
	}
	if l.Snapshot().Accounts[0].Name != "a" || c.Snapshot().Accounts[0].Name != "b" {
		t.Fatal("clone shares state with original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
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
				_ = l.ValidateBatch(Batch{Ops: []Op{{Add, k, 1, 0}}})
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := l.Stats()
	if s.Accounts != 16 || s.Generation != 16*50 {
		t.Fatalf("%+v", s)
	}
	var total int64
	for _, a := range l.Snapshot().Accounts {
		total += a.Value
	}
	if total != 16*50 {
		t.Fatalf("total=%d", total)
	}
}
