package balanceledger297

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {-1, 8, 10}, {4, 8, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "toolongname", "UPPER"}
	for _, n := range bad {
		if e := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	good := []string{"a", "z0-_", "12345678", "-", "_"}
	for _, n := range good {
		if e := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	cases := []Op{
		{Kind(0), "a", 1, 0},  // unknown kind
		{Kind(99), "a", 1, 0}, // unknown kind
		{Add, "a", 0, 0},      // zero delta
		{Add, "a", 1, 1},      // extra Value
		{Set, "a", 1, 1},      // extra Delta
		{Delete, "a", 1, 0},   // extra Delta
		{Delete, "a", 0, 1},   // extra Value
	}
	for _, op := range cases {
		if e := l.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	// ValidateBatch must not mutate state.
	if s := l.Snapshot(); s.Generation != 0 || len(s.Accounts) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestOverflowGuards(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	// Adding past MaxInt64 must be detected before arithmetic.
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("overflow up: %v", e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("overflow down: %v", e)
	}
	// Abs limit: MinInt64 cannot be negated; must be rejected, not overflow.
	l2, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("minint64: %v", e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatalf("minint64 delta: %v", e)
	}
	if len(l2.Snapshot().Accounts) != 0 {
		t.Fatal("failed batch leaked state")
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{}) // empty batch: no generation bump
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	// Failed batch must not bump generation or consume revisions.
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "nope", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	if r.Generation != 2 || r.Revision != 3 {
		t.Fatal(r)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(4)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "a", "b", "d"} // value desc, name asc on ties
	for i, w := range want {
		if top[i].Name != w {
			t.Fatalf("top[%d]=%s want %s (%v)", i, top[i].Name, w, top)
		}
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("negative n: %v", e)
	}
	// Mutating the returned slice must not corrupt internal state.
	top[0].Value = -999
	again, _ := l.Top(1)
	if again[0].Value != 9 {
		t.Fatal("returned slice aliases internal state")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 3}}})
	c, e := l.Clone()
	if e != nil {
		t.Fatal(e)
	}
	// Logical clocks are preserved.
	if !reflect.DeepEqual(c.Snapshot(), l.Snapshot()) {
		t.Fatal("clone diverges at copy time")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "z", 0, 1}}})
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 1 {
		t.Fatal("clone not independent")
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "z", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal("clone write leaked into original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_ = l.Stats()
				_, _ = l.Top(8)
				_ = l.Snapshot()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	s := l.Snapshot()
	if st.Accounts != len(s.Accounts) || st.Generation != s.Generation || st.NextRevision != s.NextRevision {
		t.Fatalf("stats/snapshot disagree: %+v vs %+v", st, s)
	}
	// 32 keys * 50 successful adds of 1.
	for _, a := range s.Accounts {
		if a.Value != 50 {
			t.Fatalf("account %s=%d, want 50", a.Name, a.Value)
		}
	}
}
