package balanceledger277

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestOptionsValidation(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {-1, 8, 10}, {4, 8, -5},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"}
	for _, n := range bad {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	good := []string{"a", "z0-_", "0", "-", "_"}
	for _, n := range good {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 0, 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unknown kind accepted")
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
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("minint abs: %v", err)
	}
	l2, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, err := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 11}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("set abs: %v", err)
	}
	if _, err := l2.Apply(Batch{Ops: []Op{{Add, "a", -20, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("add abs: %v", err)
	}
	if got := l2.Snapshot().Generation; got != 0 {
		t.Fatalf("failed batches changed generation: %d", got)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatalf("rollback left %d accounts", n)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); err != nil {
		t.Fatal(err)
	}
	if g := l.Snapshot().Generation; g != 1 {
		t.Fatalf("generation: %d", g)
	}
}

func TestRevisionSequence(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Add, "b", 5, 0}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Revision != 2 {
		t.Fatalf("revision: %d", r.Revision)
	}
	s := l.Snapshot()
	if s.NextRevision != 3 || s.Accounts[0].Revision != 2 {
		t.Fatalf("snapshot: %+v", s)
	}
}

func TestTopOrderAndIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1}}})
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
	top[0].Value = -100
	if l.Snapshot().Accounts[2].Value != 9 {
		t.Fatal("returned slice aliases internal state")
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative n: %v", err)
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
		t.Fatal("clone clocks diverge")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if len(l.Snapshot().Accounts) != 1 || len(c.Snapshot().Accounts) != 0 {
		t.Fatal("clone shares state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
				_ = l.Stats()
				if j%25 == 0 {
					_, _ = l.Clone()
				}
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
			}
		}()
	}
	wg.Wait()
	st := l.Stats()
	if st.Accounts != 32 || st.NextRevision != 32*50+1 || st.Generation != 32*50 {
		t.Fatalf("stats: %+v", st)
	}
}
