package rewardledger

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxAccounts: 1, MaxNameBytes: 1, MaxAbsValue: 1}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
	for _, o := range []Options{
		{MaxAccounts: 0, MaxNameBytes: 1, MaxAbsValue: 1},
		{MaxAccounts: -1, MaxNameBytes: 1, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 0, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: -2, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 1, MaxAbsValue: 0},
		{MaxAccounts: 1, MaxNameBytes: 1, MaxAbsValue: -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: got %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 20})
	for _, name := range []string{"", "A", "a b", "a.b", "中文", "toolongname", "a/b", "+x"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", name, e)
		}
	}
	for _, name := range []string{"a", "z-0_9", "abcdefgh", "-", "_"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}}); e != nil {
			t.Fatalf("name %q: got %v", name, e)
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
	// Structural validation of the whole batch happens before state reads:
	// a bad op later in the batch must fail even if an earlier op would
	// have failed against state too (Delete of missing account).
	_, e := l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}, {Kind(7), "a", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	// Adding 1 would overflow int64: detected before arithmetic.
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// MinInt64+1 is exactly -MaxAbsValue and allowed.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Delta of MinInt64 must not overflow the check itself.
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// MaxInt64 + MinInt64 = -1 is exactly representable and allowed.
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}); e != nil {
		t.Fatal(e)
	}

	small, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := small.Apply(Batch{Ops: []Op{{Set, "x", 0, 11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := small.Apply(Batch{Ops: []Op{{Set, "x", 0, -11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := small.Apply(Batch{Ops: []Op{{Set, "x", 0, -10}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := small.Apply(Batch{Ops: []Op{{Add, "x", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Add, "c", 3, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	if x.Generation != 1 || x.Revision != 3 {
		t.Fatal(x)
	}
	// Empty batch: generation unchanged, no error.
	y, e := l.Apply(Batch{})
	if e != nil || y.Generation != 1 {
		t.Fatal(y, e)
	}
	// Failed batch: generation and revisions unchanged.
	if _, e = l.Apply(Batch{Ops: []Op{{Add, "d", 1, 0}, {Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 4 {
		t.Fatal(s)
	}
	z, _ := l.Apply(Batch{Ops: []Op{{Add, "b", 1, 0}}})
	if z.Generation != 2 || z.Revision != 4 {
		t.Fatal(z)
	}
	// Changed reports final state of surviving touched accounts.
	if len(x.Changed) != 2 || x.Changed[0].Name != "b" || x.Changed[1].Name != "c" {
		t.Fatal(x.Changed)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	before := l.Snapshot()
	// Exceeds capacity only at the end: whole batch rolls back.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Delete, "a", 0, 0}, {Set, "d", 0, 4}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("state mutated after failed batch")
	}
	// Net-zero capacity change succeeds even though it peaks mid-batch.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Delete, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, e := l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	if e != nil {
		t.Fatal(e)
	}
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(e, top)
	}
	want := []string{"c", "a", "b"} // value desc, name asc on ties
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatal(all)
	}
	if zero, _ := l.Top(0); len(zero) != 0 {
		t.Fatal(zero)
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Mutating the returned slice must not affect the ledger.
	top[0].Value = -999
	again, _ := l.Top(1)
	if again[0].Value != 9 {
		t.Fatal("returned slice aliases internal state")
	}
	snap := l.Snapshot()
	snap.Accounts[0].Value = -999
	if l.Snapshot().Accounts[0].Value == -999 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestSnapshotSorted(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "m", 0, 1}, {Set, "a", 0, 1}, {Set, "z", 0, 1}}})
	s := l.Snapshot()
	if len(s.Accounts) != 3 || s.Accounts[0].Name != "a" || s.Accounts[1].Name != "m" || s.Accounts[2].Name != "z" {
		t.Fatal(s.Accounts)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	const workers = 16
	var w sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%13)) + string(rune('a'+(i/13)%13))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Set, k, 0, int64(j)}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	// Every applied op is atomic: revisions are consecutive and unique.
	seen := map[uint64]bool{}
	for _, a := range s.Accounts {
		if a.Revision == 0 || a.Revision >= s.NextRevision || seen[a.Revision] {
			t.Fatal(a, s.NextRevision)
		}
		seen[a.Revision] = true
	}
	if s.Generation == 0 {
		t.Fatal("generation did not advance")
	}
}

func TestConcurrentSameAccount(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	const n = 100
	var w sync.WaitGroup
	for i := 0; i < n; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			if _, e := l.Apply(Batch{Ops: []Op{{Add, "acct", 1, 0}}}); e != nil {
				t.Error(e)
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != n {
		t.Fatal(s.Accounts)
	}
	if s.Generation != n || s.NextRevision != n+1 {
		t.Fatal(s.Generation, s.NextRevision)
	}
}
