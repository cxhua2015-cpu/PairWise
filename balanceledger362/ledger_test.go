package balanceledger362

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func opt() Options { return Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100} }

func mustNew(t *testing.T, o Options) *Ledger {
	t.Helper()
	l, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{MaxAccounts: 0, MaxNameBytes: 8, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 0, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 0},
		{MaxAccounts: -1, MaxNameBytes: 8, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: -5},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, err)
		}
	}
}

func TestInvalidNames(t *testing.T) {
	l := mustNew(t, opt())
	for _, name := range []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"} {
		_, err := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", name, err)
		}
	}
	for _, name := range []string{"a", "z-9_", "0", "abcdefgh"} {
		if _, err := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); err != nil {
			t.Fatalf("name %q: unexpected %v", name, err)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := mustNew(t, opt())
	_, err := l.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}, {Set, "b", 0, 1}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if got := len(l.Snapshot().Accounts); got != 0 {
		t.Fatalf("structural failure must not mutate state, got %d accounts", got)
	}
}

func TestOverflowAdd(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal("positive overflow not detected")
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", -2, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal("negative overflow not detected")
	}
	if got := l.Snapshot().Accounts[0].Value; got != -math.MaxInt64 {
		t.Fatalf("state mutated after failed batch: %d", got)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatal("MinInt64 exceeds abs limit of MaxInt64")
	}
	if got := l.Snapshot().Accounts[0].Value; got != -math.MaxInt64 {
		t.Fatalf("state mutated after failed batch: %d", got)
	}
}

func TestAbsLimit(t *testing.T) {
	l := mustNew(t, opt())
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 101}}}); !errors.Is(err, ErrValue) {
		t.Fatal("set beyond limit")
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", -101, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal("add beyond negative limit")
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 100}, {Add, "a", -201, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal("mid-batch overflow of abs limit")
	}
	if got := len(l.Snapshot().Accounts); got != 0 {
		t.Fatalf("rollback failed, %d accounts", got)
	}
}

func TestCapacityOnlyAtBatchEnd(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 10})
	// Temporarily exceeds capacity mid-batch, ends within limit: must succeed.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	// Ends above capacity: must fail and roll back.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Name != "b" {
		t.Fatal(s.Accounts)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := mustNew(t, opt())
	r0, err := l.Apply(Batch{})
	if err != nil || r0.Generation != 0 {
		t.Fatal("empty batch must not bump generation", r0, err)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Add, "b", 2, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	// Delete consumes no revision.
	r2, _ := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if r2.Generation != 2 || r2.Revision != 2 {
		t.Fatal(r2)
	}
	if s := l.Snapshot(); s.NextRevision != 3 || s.Generation != 2 {
		t.Fatal(s)
	}
	// Failed batch bumps neither.
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if s := l.Snapshot(); s.NextRevision != 3 || s.Generation != 2 {
		t.Fatal("failed batch changed counters", s)
	}
}

func TestChangedContents(t *testing.T) {
	l := mustNew(t, opt())
	r, err := l.Apply(Batch{Ops: []Op{
		{Set, "a", 0, 1},
		{Add, "a", 4, 0},
		{Set, "b", 0, 9},
		{Delete, "b", 0, 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "a" || r.Changed[0].Value != 5 {
		t.Fatalf("changed = %+v", r.Changed)
	}
}

func TestTopOrdering(t *testing.T) {
	l := mustNew(t, opt())
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 7}, {Set, "d", 0, -1},
	}})
	top, err := l.Top(3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"c", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatalf("top[%d] = %q, want %q", i, top[i].Name, w)
		}
	}
	if n, _ := l.Top(100); len(n) != 4 {
		t.Fatal("Top larger than account count")
	}
	if n, _ := l.Top(0); len(n) != 0 {
		t.Fatal("Top(0)")
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("Top(-1) should fail")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := mustNew(t, opt())
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	top, _ := l.Top(2)
	top[0].Value = -999
	r, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	r.Changed[0].Value = 12345
	got := l.Snapshot()
	if got.Accounts[0].Value != 2 || got.Accounts[1].Value != 2 {
		t.Fatalf("internal state leaked via returned slices: %+v", got.Accounts)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := l.Snapshot()
	var total int64
	for _, a := range s.Accounts {
		total += a.Value
	}
	if total != 16*50 {
		t.Fatalf("total = %d, want %d", total, 16*50)
	}
	if s.NextRevision != 16*50+1 {
		t.Fatalf("nextRevision = %d", s.NextRevision)
	}
}

func TestConcurrentDisjointAccounts(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a'+i/26)) + string(rune('a'+i%26))
			for j := 0; j < 20; j++ {
				if _, err := l.Apply(Batch{Ops: []Op{{Add, name, 2, 0}}}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	for _, a := range l.Snapshot().Accounts {
		if a.Value != 40 {
			t.Fatalf("%s = %d, want 40", a.Name, a.Value)
		}
	}
}
