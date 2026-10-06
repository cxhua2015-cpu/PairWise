package balanceledger212

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	l := led(t)
	for _, b := range []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Kind: Add, Name: ""}}},
		{Ops: []Op{{Kind: Add, Name: "Upper"}}},
		{Ops: []Op{{Kind: Add, Name: "has space"}}},
		{Ops: []Op{{Kind: Add, Name: "dot.com"}}},
		{Ops: []Op{{Kind: Add, Name: "waytoolongname"}}},
	} {
		if _, e := l.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	// Structural validation happens before state reads: an invalid op
	// anywhere in the batch must fail even if earlier ops would fail too.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}, {Kind: 0, Name: "x"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// MinInt64 delta on a fresh account must not overflow the check itself.
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "c", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed batches roll back: "a" keeps its value, "b" never existed.
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != math.MaxInt64 {
		t.Fatal(s)
	}
	if s.Generation != 1 {
		t.Fatal(s.Generation)
	}
}

func TestAbsLimitEnforced(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", -40, 0}}}); e != nil {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != -20 {
		t.Fatal(got)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
	// Mid-batch overflow is fine as long as the final count fits.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}, {Delete, "c", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r0, e := l.Apply(Batch{})
	if e != nil || r0.Generation != 0 || r0.Revision != 0 {
		t.Fatal(r0, e)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "a", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	// Failed batch leaves generation and revision untouched.
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestChangedAndDelete(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r.Changed)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	want := []string{"c", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "d" {
		t.Fatal(all)
	}
	// Returned slices must be isolated from internal state.
	all[0].Value = -99
	again, _ := l.Top(1)
	if again[0].Value != 9 {
		t.Fatal(again)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	var total int64
	for _, a := range s.Accounts {
		total += a.Value
	}
	if total != 32*50 {
		t.Fatal(total, len(s.Accounts))
	}
	if s.Generation != 32*50 {
		t.Fatal(s.Generation)
	}
}
