package balanceledger217

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func mustLedger(t *testing.T, o Options) *Ledger {
	t.Helper()
	l, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0},
		{-1, 8, 10}, {4, -1, 10}, {4, 8, -1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	bad := []Op{
		{Kind(0), "a", 1, 0},
		{Kind(4), "a", 1, 0},
		{Add, "", 1, 0},
		{Add, "A", 1, 0},
		{Add, "a b", 1, 0},
		{Add, "a/b", 1, 0},
		{Add, "toolongname", 1, 0}, // > MaxNameBytes(8)
		{Add, "中文", 1, 0},
	}
	for _, op := range bad {
		if _, err := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: want ErrInvalidInput, got %v", op, err)
		}
	}
	// Valid names: digits, hyphen, underscore.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a1-_", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	// Structural validation of the whole batch happens before any state read:
	// a bad op late in the batch must not apply earlier ops.
	before := l.Snapshot()
	_, err := l.Apply(Batch{Ops: []Op{{Set, "x", 0, 1}, {Kind(9), "y", 0, 0}}})
	if !errors.Is(err, ErrInvalidInput) || !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal(err)
	}
}

func TestEmptyBatch(t *testing.T) {
	l := led(t)
	r0, err := l.Apply(Batch{})
	if err != nil || r0.Generation != 0 || r0.Revision != 0 || len(r0.Changed) != 0 {
		t.Fatal(r0, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	r, err := l.Apply(Batch{Ops: nil})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
}

func TestOverflow(t *testing.T) {
	l, err := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	// Failed batches roll back revisions too.
	s := l.Snapshot()
	if s.NextRevision != 3 || len(s.Accounts) != 2 {
		t.Fatal(s)
	}
}

func TestAbsLimit(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "new", 21, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t) // MaxAccounts 4
	ops := []Op{}
	for _, n := range []string{"a", "b", "c", "d"} {
		ops = append(ops, Op{Set, n, 0, 1})
	}
	if _, err := l.Apply(Batch{Ops: ops}); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	// Net-new account within one batch exceeds capacity only at the end.
	_, err := l.Apply(Batch{Ops: []Op{{Set, "e", 0, 1}}})
	if !errors.Is(err, ErrCapacity) || !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal(err)
	}
	// Delete-then-create within one batch fits.
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "e", 0, 2}}}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteSemantics(t *testing.T) {
	l := led(t)
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "ghost", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Create then delete within one batch: no account remains, revision consumed.
	r, err := l.Apply(Batch{Ops: []Op{{Set, "tmp", 0, 1}, {Delete, "tmp", 0, 0}}})
	if err != nil || r.Revision != 1 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if len(l.Snapshot().Accounts) != 0 {
		t.Fatal(l.Snapshot())
	}
	// Delete then re-add in one batch starts from the delta.
	r, err = l.Apply(Batch{Ops: []Op{{Set, "k", 0, 5}, {Delete, "k", 0, 0}, {Add, "k", 2, 0}}})
	if err != nil || len(r.Changed) != 1 || r.Changed[0].Value != 2 {
		t.Fatal(r, err)
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
	want := []string{"c", "a", "b"} // value desc, name asc on ties
	for i, n := range want {
		if top[i].Name != n {
			t.Fatal(top)
		}
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatal(all)
	}
	if zero, _ := l.Top(0); len(zero) != 0 {
		t.Fatal(zero)
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r.Changed[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 99
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	again := l.Snapshot()
	if again.Accounts[0].Value != 1 {
		t.Fatal(again)
	}
}

func TestRevisionSequence(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Add, "b", 1, 0}}})
	if err != nil || r.Revision != 3 || r.Generation != 1 {
		t.Fatal(r, err)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Changed[0].Revision != 3 || r.Changed[0].Value != 3 {
		t.Fatal(r.Changed)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 4 {
		t.Fatal(s)
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
				_, _ = l.Top(10)
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
	if total != 32*50 {
		t.Fatal(total)
	}
	if s.NextRevision != uint64(32*50)+1 || s.Generation != 32*50 {
		t.Fatal(s)
	}
}

func TestConcurrentExclusiveBatches(t *testing.T) {
	// Two writers racing on disjoint accounts; every batch must apply fully.
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var wg sync.WaitGroup
	for _, name := range []string{"x", "y"} {
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if _, err := l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	for _, a := range l.Snapshot().Accounts {
		if a.Value != 100 {
			t.Fatal(a)
		}
	}
}
