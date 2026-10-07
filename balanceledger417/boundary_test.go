package balanceledger417

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {-1, 8, 10}, {4, 8, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a", Delta: 1}}},  // unknown kind
		{Ops: []Op{{Kind: 99, Name: "a", Delta: 1}}}, // unknown kind
		{Ops: []Op{{Add, "", 1, 0}}},                 // empty name
		{Ops: []Op{{Add, "Upper", 1, 0}}},            // uppercase
		{Ops: []Op{{Add, "a b", 1, 0}}},              // space
		{Ops: []Op{{Add, "toolongname", 1, 0}}},      // > MaxNameBytes
		{Ops: []Op{{Add, "a", 0, 0}}},                // zero delta
		{Ops: []Op{{Add, "a", 1, 1}}},                // extra field
		{Ops: []Op{{Set, "a", 1, 0}}},                // extra field
		{Ops: []Op{{Set, "a", 0, 0}}},                // zero value
		{Ops: []Op{{Delete, "a", 1, 0}}},             // extra field
		{Ops: []Op{{Set, "a", 0, 21}}},               // abs limit
		{Ops: []Op{{Set, "a", 0, -21}}},              // abs limit
	}
	for i, b := range bad {
		if e := l.ValidateBatch(b); e == nil {
			t.Fatalf("case %d accepted", i)
		}
		if _, e := l.Apply(b); e == nil {
			t.Fatalf("apply case %d accepted", i)
		}
	}
	if s := l.Stats(); s.Generation != 0 || s.Accounts != 0 {
		t.Fatalf("rejected batches mutated state: %+v", s)
	}
	good := Batch{Ops: []Op{{Add, "a-1_", 1, 0}, {Set, "b", 0, -20}, {Delete, "a-1_", 0, 0}}}
	if e := l.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
}

func TestOverflowGuards(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("positive overflow: %v", e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -2, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("negative overflow: %v", e)
	}
	// MinInt64 magnitude must not panic on abs checks.
	l2, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("MinInt64 set: %v", e)
	}
}

func TestAbsLimitOnAdd(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}, {Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if l.Stats().Accounts != 0 {
		t.Fatal("failed batches leaked accounts")
	}
}

func TestGenerationAndRevisionClocks(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{}); e != nil {
		t.Fatal(e)
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatalf("empty batch bumped generation to %d", g)
	}
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	// Failed batch must not advance either clock.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s2 := l.Snapshot(); s2.Generation != 1 || s2.NextRevision != 3 {
		t.Fatal(s2)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -3},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(e, top)
	}
	want := []string{"c", "a", "b"} // value desc, name asc on ties
	for i, n := range want {
		if top[i].Name != n {
			t.Fatalf("top[%d]=%s want %s (%v)", i, top[i].Name, n, top)
		}
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Mutating the returned slice must not affect the ledger.
	top[0].Value = -100
	again, _ := l.Top(1)
	if again[0].Value != 9 {
		t.Fatal("returned slice aliases internal state")
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "z", 0, 1}, {Set, "m", 0, 2}, {Set, "a", 0, 3}}})
	s := l.Snapshot()
	if len(s.Accounts) != 3 || s.Accounts[0].Name != "a" || s.Accounts[1].Name != "m" || s.Accounts[2].Name != "z" {
		t.Fatal(s.Accounts)
	}
	s.Accounts[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 3 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	l := led(t)
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 7}}})
	c, e := l.Clone()
	if e != nil {
		t.Fatal(e)
	}
	cs := c.Snapshot()
	if cs.Generation != r.Generation || cs.NextRevision != 2 || cs.Accounts[0].Revision != 1 {
		t.Fatalf("clone lost logical clocks: %+v", cs)
	}
	// Mutate both; neither may observe the other.
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	_, _ = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	if c.Stats().Accounts != 0 || l.Stats().Accounts != 1 {
		t.Fatal("clone and original share state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
				_ = l.Stats()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Add, k, 1, 0}}})
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	if int(st.Accounts) != len(l.Snapshot().Accounts) {
		t.Fatal("stats/snapshot inconsistent")
	}
	// Every successful non-empty batch bumps generation exactly once and
	// every Add consumes exactly one revision: NextRevision-1 >= Generation.
	if st.NextRevision-1 < st.Generation {
		t.Fatalf("clock skew: %+v", st)
	}
}
