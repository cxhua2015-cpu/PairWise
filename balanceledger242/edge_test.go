package balanceledger242

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

func TestValidateBatchStructural(t *testing.T) {
	l := led(t)
	cases := []struct {
		op   Op
		want error
	}{
		{Op{Kind: 0, Name: "a", Delta: 1}, ErrInvalidInput},
		{Op{Kind: 99, Name: "a", Delta: 1}, ErrInvalidInput},
		{Op{Kind: Add, Name: "a"}, ErrInvalidInput},                     // zero delta
		{Op{Kind: Add, Name: "a", Delta: 1, Value: 1}, ErrInvalidInput}, // extra field
		{Op{Kind: Set, Name: "a", Delta: 1}, ErrInvalidInput},           // extra field
		{Op{Kind: Delete, Name: "a", Delta: 1}, ErrInvalidInput},        // extra field
		{Op{Kind: Add, Name: "", Delta: 1}, ErrInvalidInput},
		{Op{Kind: Add, Name: "A", Delta: 1}, ErrInvalidInput},
		{Op{Kind: Add, Name: "a b", Delta: 1}, ErrInvalidInput},
		{Op{Kind: Add, Name: "toolongname", Delta: 1}, ErrInvalidInput},
		{Op{Kind: Set, Name: "a", Value: 21}, ErrValue}, // exceeds MaxAbsValue=20
		{Op{Kind: Set, Name: "a", Value: -20}, nil},
		{Op{Kind: Add, Name: "ok_nm-1", Delta: -3}, nil},
		{Op{Kind: Delete, Name: "z"}, nil},
	}
	for _, c := range cases {
		if err := l.ValidateBatch(Batch{Ops: []Op{c.op}}); !errors.Is(err, c.want) {
			t.Fatalf("op %+v: got %v want %v", c.op, err, c.want)
		}
	}
	// Validation is side-effect free.
	if s := l.Snapshot(); s.Generation != 0 || len(s.Accounts) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestOverflowDetectedBeforeArithmetic(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("positive overflow: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", -2, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("negative overflow: %v", err)
	}
	// Failed batches roll back: value must remain MinInt64+1.
	if got := l.Snapshot().Accounts[0].Value; got != math.MinInt64+1 {
		t.Fatalf("rollback broken: %d", got)
	}
}

func TestAbsLimitAndCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 5})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 5}, {Set, "b", 0, -5}}}); err != nil {
		t.Fatal(err)
	}
	// Exceeds final capacity (c would be a third account): full rollback.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 4}, {Set, "c", 0, 1}}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	s := l.Snapshot()
	if s.Generation != 1 || len(s.Accounts) != 2 || s.Accounts[0].Value != 5 {
		t.Fatalf("rollback broken: %+v", s)
	}
	// Abs limit inside batch rolls back earlier ops too.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Add, "b", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("abs limit: %v", err)
	}
	if l.Snapshot().Accounts[0].Value != 5 {
		t.Fatal("abs-limit failure leaked partial writes")
	}
}

func TestGenerationAndRevisionSemantics(t *testing.T) {
	l := led(t)
	r0, err := l.Apply(Batch{}) // empty batch: no generation bump
	if err != nil || r0.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r0, err)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "b", 1, 0}, {Set, "a", 0, 2}}})
	if r1.Generation != 1 || r1.Revision != 3 {
		t.Fatalf("revisions: %+v", r1)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "nope", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	z := l.Stats()
	if z.Generation != 1 || z.NextRevision != 4 || z.Accounts != 2 {
		t.Fatalf("stats after failed batch: %+v", z)
	}
	// Delete consumes no revision.
	r2, _ := l.Apply(Batch{Ops: []Op{{Delete, "b", 0, 0}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatalf("delete batch: %+v", r2)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 {
		t.Fatal(top, err)
	}
	want := []string{"c", "a", "b"} // value desc, then name asc
	for i, name := range want {
		if top[i].Name != name {
			t.Fatalf("top[%d]=%s want %s", i, top[i].Name, name)
		}
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative n: %v", err)
	}
	if top, _ := l.Top(0); len(top) != 0 {
		t.Fatal(top)
	}
}

func TestReturnedSlicesAreIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	snap := l.Snapshot()
	snap.Accounts[0].Value = 999
	top, _ := l.Top(1)
	top[0].Value = 999
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatalf("internal state mutated via returned slice: %d", got)
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
		t.Fatal("clone must preserve logical clocks")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "b", 0, 1}}})
	if len(l.Snapshot().Accounts) != 1 || len(c.Snapshot().Accounts) != 1 {
		t.Fatal("clone and source diverged incorrectly")
	}
	if l.Snapshot().Accounts[0].Name != "a" || c.Snapshot().Accounts[0].Name != "b" {
		t.Fatal("clone aliases source")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, name, 0, 1}}})
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	wg.Wait()
	z := l.Stats()
	if z.Accounts != 32 || z.Generation != 32*50 {
		t.Fatalf("stats: %+v", z)
	}
	// Every successful batch bumps generation exactly once.
	if got := l.Snapshot().Generation; got != 1600 {
		t.Fatalf("generation=%d", got)
	}
}
