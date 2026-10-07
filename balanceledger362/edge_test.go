package balanceledger362

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, 1, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	l := led(t)
	bad := []Op{
		{Kind: 0, Name: "a"}, {Kind: 99, Name: "a"},
		{Kind: Add, Name: ""}, {Kind: Add, Name: "Upper"},
		{Kind: Add, Name: "with space"}, {Kind: Add, Name: "toolongname"},
		{Kind: Set, Name: "é"},
	}
	for _, op := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	// structural validation happens before any state read: even a
	// would-be-NotFound batch must report ErrInvalidInput first.
	_, e := l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}, {Kind: 7, Name: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -math.MaxInt64}, {Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	l2 := led(t) // MaxAbsValue 20
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", -41, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	b := l.Snapshot()
	_, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := l.Snapshot(); got.Generation != b.Generation || len(got.Accounts) != 2 {
		t.Fatal(got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r0, e := l.Apply(Batch{})
	if e != nil || r0.Generation != 0 {
		t.Fatal(r0, e)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r2, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Delete, "a", 0, 0}}})
	if r1.Generation != 1 || r2.Generation != 2 || r2.Revision != 2 {
		t.Fatal(r1, r2)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 3 || len(s.Accounts) != 0 {
		t.Fatal(s)
	}
}

func TestChangedAndOrdering(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 9}, {Add, "b", 1, 0}, {Set, "c", 0, 5},
	}})
	if e != nil || len(x.Changed) != 3 {
		t.Fatal(e, x)
	}
	if x.Changed[0].Name != "b" || x.Changed[0].Value != 6 || x.Changed[0].Revision != 3 {
		t.Fatal(x.Changed)
	}
	top, _ := l.Top(10)
	if len(top) != 3 || top[0].Name != "a" || top[1].Name != "b" || top[2].Name != "c" {
		t.Fatal(top)
	}
	s := l.Snapshot()
	if s.Accounts[0].Name != "a" || s.Accounts[1].Name != "b" || s.Accounts[2].Name != "c" {
		t.Fatal(s)
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	top, _ := l.Top(1)
	top[0].Value = 999
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("internal state mutated via returned slice")
	}
}

func TestConcurrentStress(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	var total int64
	for _, a := range l.Snapshot().Accounts {
		total += a.Value
	}
	if total != 16*50 {
		t.Fatal(total)
	}
}
