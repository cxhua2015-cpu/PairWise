package balanceledger327

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func mustNew(t *testing.T, o Options) *Ledger {
	t.Helper()
	l, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestNewInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {4, 8, -1}, {-1, 8, 10},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, err)
		}
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	l := mustNew(t, Options{MaxAccounts: 4, MaxNameBytes: 4, MaxAbsValue: 10})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	// Unknown kind and bad name must fail even though earlier ops are valid.
	for _, ops := range [][]Op{
		{{Add, "a", 1, 0}, {Kind(0), "b", 0, 0}},
		{{Add, "a", 1, 0}, {Add, "Bad", 1, 0}},
		{{Add, "a", 1, 0}, {Add, "toolongname", 1, 0}},
		{{Add, "a", 1, 0}, {Add, "", 1, 0}},
	} {
		if _, err := l.Apply(Batch{Ops: ops}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("ops %v: want ErrInvalidInput, got %v", ops, err)
		}
	}
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatalf("state mutated by invalid batch: %d", got)
	}
	if g := l.Snapshot().Generation; g != 1 {
		t.Fatalf("generation advanced on failed batch: %d", g)
	}
}

func TestValidNames(t *testing.T) {
	l := mustNew(t, Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 10})
	_, err := l.Apply(Batch{Ops: []Op{
		{Set, "a-b_c9", 0, 1}, {Set, "0", 0, 2}, {Set, "_", 0, 3}, {Set, "-", 0, 4},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"A", "a b", "a.b", "é", "a/b"} {
		if _, err := l.Apply(Batch{Ops: []Op{{Set, bad, 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", bad, err)
		}
	}
}

func TestAbsLimitAndOverflow(t *testing.T) {
	l := mustNew(t, Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	// Input beyond abs limit rejected before arithmetic.
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 11, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -11}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	// Resulting value beyond limit rejected.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 10}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", -20, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if got := l.Snapshot().Accounts[0].Value; got != 10 {
		t.Fatalf("value mutated: %d", got)
	}
}

func TestOverflowAtInt64Boundary(t *testing.T) {
	l := mustNew(t, Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -math.MaxInt64}, {Add, "b", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	// Extreme but in-range arithmetic must succeed.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "c", 0, math.MaxInt64}, {Add, "c", -math.MaxInt64, 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationAndRevisionSemantics(t *testing.T) {
	l := mustNew(t, Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	// Empty batch: no generation bump.
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, err = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Add, "a", 1, 0}}})
	if err != nil || r.Generation != 1 || r.Revision != 3 {
		t.Fatalf("batch: %+v %v", r, err)
	}
	// Delete does not consume a revision.
	r, err = l.Apply(Batch{Ops: []Op{{Delete, "b", 0, 0}}})
	if err != nil || r.Generation != 2 || r.Revision != 3 {
		t.Fatalf("delete batch: %+v %v", r, err)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatalf("snapshot: %+v", s)
	}
	// Failed batch leaves generation and revision untouched.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "x", 0, 1}, {Delete, "missing", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if s := l.Snapshot(); s.Generation != 2 || s.NextRevision != 4 {
		t.Fatalf("failed batch advanced counters: %+v", s)
	}
}

func TestChangedReflectsFinalState(t *testing.T) {
	l := mustNew(t, Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	r, err := l.Apply(Batch{Ops: []Op{
		{Set, "a", 0, 1}, {Add, "a", 4, 0}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Set, "a", 0, 9},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Account{
		{Name: "a", Value: 9, Revision: 4},
		{Name: "b", Value: 2, Revision: 3},
	}
	if !reflect.DeepEqual(r.Changed, want) {
		t.Fatalf("changed: got %+v want %+v", r.Changed, want)
	}
}

func TestCapacityRollback(t *testing.T) {
	l := mustNew(t, Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	// Delete-then-create within one batch fits capacity.
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "c", 0, 3}}}); err != nil {
		t.Fatal(err)
	}
	names := []string{l.Snapshot().Accounts[0].Name, l.Snapshot().Accounts[1].Name}
	if !reflect.DeepEqual(names, []string{"b", "c"}) {
		t.Fatal(names)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l := mustNew(t, Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, err := l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -1}, {Set, "d", 0, 7},
	}})
	if err != nil {
		t.Fatal(err)
	}
	top, err := l.Top(3)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{top[0].Name, top[1].Name, top[2].Name}
	if !reflect.DeepEqual(names, []string{"d", "a", "b"}) {
		t.Fatalf("top order: %v", names)
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatalf("top beyond size: %d", len(all))
	}
	if empty, err := l.Top(0); err != nil || len(empty) != 0 {
		t.Fatalf("top(0): %v %v", empty, err)
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("top(-1): %v", err)
	}
	// Mutating returned slice must not affect the ledger.
	top[0].Value = -999
	again, _ := l.Top(1)
	if again[0].Value != 7 {
		t.Fatal("returned slice aliases internal state")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := mustNew(t, Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}, {Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	s := l.Snapshot()
	if s.Accounts[0].Name != "a" || s.Accounts[1].Name != "b" {
		t.Fatalf("snapshot not name-sorted: %+v", s.Accounts)
	}
	s.Accounts[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l := mustNew(t, Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
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
	if len(s.Accounts) != workers {
		t.Fatalf("accounts: %d", len(s.Accounts))
	}
	var total int64
	for _, a := range s.Accounts {
		total += a.Value
	}
	if total != workers*50 {
		t.Fatalf("lost updates: total=%d", total)
	}
	// Revisions are unique and contiguous from 1.
	seen := make(map[uint64]bool)
	for _, a := range s.Accounts {
		if seen[a.Revision] {
			t.Fatalf("duplicate revision %d", a.Revision)
		}
		seen[a.Revision] = true
	}
	if s.NextRevision != uint64(workers*50)+1 {
		t.Fatalf("next revision: %d", s.NextRevision)
	}
}
