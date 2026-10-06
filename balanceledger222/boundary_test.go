package balanceledger222

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

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	bad := []Op{
		{Kind: 0, Name: "a"},
		{Kind: 99, Name: "a"},
		{Add, "a", 0, 0},           // zero delta
		{Add, "a", 1, 1},           // stray value
		{Set, "a", 1, 1},           // stray delta
		{Delete, "a", 1, 0},        // stray delta
		{Delete, "a", 0, 1},        // stray value
		{Add, "", 1, 0},            // empty name
		{Add, "A", 1, 0},           // uppercase
		{Add, "a b", 1, 0},         // space
		{Add, "a.b", 1, 0},         // dot
		{Add, "toolongname", 1, 0}, // over MaxNameBytes
	}
	for _, op := range bad {
		if err := l.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
		if _, err := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply op %+v: %v", op, err)
		}
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "ok-1_", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	// Failed validation must not change state.
	if s := l.Snapshot(); s.Generation != 0 || len(s.Accounts) != 0 {
		t.Fatalf("state mutated: %+v", s)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("positive overflow: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("min int64 set: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -5}, {Add, "b", math.MinInt64, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("negative overflow: %v", err)
	}
	// Rollback: a still holds MaxInt64, b never created.
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != math.MaxInt64 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestAbsLimitEnforced(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 15, 0}, {Add, "a", 6, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}}}); err != nil {
		t.Fatal(err)
	}
}

func TestCapacityOnlyAtBatchEnd(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); err != nil {
		t.Fatal(err)
	}
	// Transiently 3 accounts, but ends at 2: allowed.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Delete, "a", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	// Ends at 3: rejected and rolled back.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "d", 0, 4}, {Set, "e", 0, 5}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := len(l.Snapshot().Accounts); got != 2 {
		t.Fatalf("rollback failed, %d accounts", got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r0, err := l.Apply(Batch{})
	if err != nil || r0.Generation != 0 || r0.Revision != 0 {
		t.Fatalf("empty batch: %+v %v", r0, err)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r2, _ := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if r1.Generation != 1 || r2.Generation != 2 {
		t.Fatalf("generations: %d %d", r1.Generation, r2.Generation)
	}
	if r2.Revision != r1.Revision {
		t.Fatalf("delete must not allocate revision: %d %d", r1.Revision, r2.Revision)
	}
	if s := l.Stats(); s.Generation != 2 || s.NextRevision != 2 || s.Accounts != 0 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 {
		t.Fatal(top, err)
	}
	want := []string{"c", "a", "b"}
	for i, name := range want {
		if top[i].Name != name {
			t.Fatalf("top[%d]=%s want %s", i, top[i].Name, name)
		}
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Mutating the returned slice must not affect the ledger.
	top[0].Value = -999
	if again, _ := l.Top(1); again[0].Value != 9 {
		t.Fatal("returned slice aliases internal state")
	}
	snap := l.Snapshot()
	snap.Accounts[0].Value = -999
	if again, _ := l.Top(4); again[3].Value != -1 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 7}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if cs, ls := c.Stats(), l.Stats(); cs != ls {
		t.Fatalf("clocks diverge: %+v %+v", cs, ls)
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "b", 0, 1}}})
	if got := len(l.Snapshot().Accounts); got != 1 {
		t.Fatal("clone mutation leaked into original")
	}
	if got := len(c.Snapshot().Accounts); got != 1 {
		t.Fatal("clone mutation lost")
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
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 16 {
		t.Fatalf("accounts: %d", len(s.Accounts))
	}
	for _, a := range s.Accounts {
		if a.Value != 50 {
			t.Fatalf("account %s value %d", a.Name, a.Value)
		}
	}
	if s.Generation != 800 || s.NextRevision != 801 {
		t.Fatalf("clocks: gen=%d rev=%d", s.Generation, s.NextRevision)
	}
}
