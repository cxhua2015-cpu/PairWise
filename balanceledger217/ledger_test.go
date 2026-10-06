package balanceledger217

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func newLed(t *testing.T, o Options) *Ledger {
	t.Helper()
	l, e := New(o)
	if e != nil {
		t.Fatal(e)
	}
	return l
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	l := newLed(t, Options{MaxAccounts: 4, MaxNameBytes: 4, MaxAbsValue: 10})
	bad := []Op{
		{Kind: 0, Name: "a"}, {Kind: 99, Name: "a"},
		{Add, "", 1, 0}, {Add, "ABC", 1, 0}, {Add, "a b", 1, 0},
		{Add, "abcde", 1, 0}, {Add, "a.b", 1, 0}, {Add, "é", 1, 0},
	}
	for _, op := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	// Structural validation happens before state reads: unknown kind plus
	// missing delete target must still report ErrInvalidInput.
	_, e := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}, {Kind: 7, Name: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l := newLed(t, Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -math.MaxInt64}, {Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Overflow inside a batch rolls everything back.
	s := l.Snapshot()
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "c", 0, 1}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot(); got.Generation != s.Generation || got.NextRevision != s.NextRevision || len(got.Accounts) != 1 {
		t.Fatal(got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := newLed(t, Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	r0, e := l.Apply(Batch{})
	if e != nil || r0.Generation != 0 {
		t.Fatal(e, r0)
	}
	if s := l.Snapshot(); s.Generation != 0 || s.NextRevision != 1 {
		t.Fatal(s)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "a", 0, 2}, {Delete, "a", 0, 0}, {Set, "b", 0, 3}}})
	if r1.Generation != 1 || r1.Revision != 3 || len(r1.Changed) != 1 || r1.Changed[0].Name != "b" {
		t.Fatal(r1)
	}
	if s := l.Snapshot(); s.Generation != 1 || s.NextRevision != 4 {
		t.Fatal(s)
	}
	// Failed batch must not bump generation or consume revisions.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "nope", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := l.Snapshot(); s.Generation != 1 || s.NextRevision != 4 {
		t.Fatal(s)
	}
}

func TestTopOrdering(t *testing.T) {
	l := newLed(t, Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(e, top)
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "d" {
		t.Fatal(all)
	}
	zero, _ := l.Top(0)
	if len(zero) != 0 {
		t.Fatal(zero)
	}
	// Mutating the returned slice must not affect internal state.
	all[0].Value = -999
	again, _ := l.Top(1)
	if again[0].Value != 9 {
		t.Fatal(again)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := newLed(t, Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 1}, {Set, "a", 0, 2}}})
	s := l.Snapshot()
	if len(s.Accounts) != 2 || s.Accounts[0].Name != "a" || s.Accounts[1].Name != "b" {
		t.Fatal(s)
	}
	s.Accounts[0].Value = 777
	if l.Snapshot().Accounts[0].Value != 2 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l := newLed(t, Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}, {Add, k, -1, 0}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 32 {
		t.Fatal(len(s.Accounts))
	}
	for _, a := range s.Accounts {
		if a.Value <= 0 {
			t.Fatal(a)
		}
	}
}
