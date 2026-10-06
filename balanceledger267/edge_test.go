package balanceledger267

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
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Add, "", 1, 0}}},
		{Ops: []Op{{Add, "Upper", 1, 0}}},
		{Ops: []Op{{Add, "a b", 1, 0}}},
		{Ops: []Op{{Add, "toolongname", 1, 0}}},
		{Ops: []Op{{Add, "a", 0, 0}}},
	}
	for i, b := range cases {
		if err := l.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "ok-1_", 1, 0}, {Set, "x", 0, -20}, {Delete, "gone", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	// Validation must not mutate state.
	if s := l.Snapshot(); s.Generation != 0 || len(s.Accounts) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("overflow: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("underflow: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "c", math.MinInt64, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("minint delta: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "c", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("minint set: %v", err)
	}
}

func TestAbsLimitEnforced(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", -40, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatalf("rollback leaked %d accounts", n)
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t) // MaxAccounts 4
	ok := Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}, {Set, "d", 0, 4}}}
	if _, err := l.Apply(ok); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	// Intermediate overflow of capacity is fine; only the final count matters.
	_, err := l.Apply(Batch{Ops: []Op{{Set, "e", 0, 5}, {Delete, "a", 0, 0}}})
	if err != nil {
		t.Fatal(err)
	}
	// Final count exceeds capacity: whole batch rolls back.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "f", 0, 6}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := l.Snapshot(); got.Generation != before.Generation+1 || len(got.Accounts) != 4 {
		t.Fatalf("bad state after rollback: %+v", got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r0, err := l.Apply(Batch{})
	if err != nil || r0.Generation != 0 {
		t.Fatal(r0, err)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "b", 1, 0}, {Set, "c", 0, 3}}})
	if r1.Generation != 1 || r1.Revision != 3 {
		t.Fatal(r1)
	}
	r2, _ := l.Apply(Batch{Ops: []Op{{Delete, "c", 0, 0}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
	// Failed batch must not advance clocks.
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if l.Snapshot().NextRevision != 4 || l.Stats().Generation != 2 {
		t.Fatal("failed batch advanced clocks")
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3}, {Set, "d", 0, 7},
	}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 {
		t.Fatal(top, err)
	}
	want := []string{"d", "a", "b"}
	for i, name := range want {
		if top[i].Name != name {
			t.Fatalf("top[%d]=%s want %s", i, top[i].Name, name)
		}
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Mutating returned slices must not affect internal state.
	top[0].Value = -99
	snap := l.Snapshot()
	snap.Accounts[0].Value = -99
	again, _ := l.Top(4)
	for _, a := range again {
		if a.Value == -99 {
			t.Fatal("returned slices alias internal state")
		}
	}
	names := []string{}
	for _, a := range l.Snapshot().Accounts {
		names = append(names, a.Name)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("snapshot not sorted: %v", names)
		}
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "z", 0, 9}}})
	if l.Stats().Accounts != 1 || l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("clone writes leaked into original")
	}
	_, _ = l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if c.Stats().Accounts != 2 {
		t.Fatal("original writes leaked into clone")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 20; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, name, 0, 1}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
				_ = l.Stats()
				if j == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	wg.Wait()
	st := l.Stats()
	if st.Accounts != 32 || st.NextRevision != 1+32*20 {
		t.Fatalf("stats: %+v", st)
	}
	snap := l.Snapshot()
	if int(st.Generation) != 32*20 || len(snap.Accounts) != 32 {
		t.Fatalf("snapshot: %+v", snap)
	}
	for _, a := range snap.Accounts {
		if a.Value <= 0 {
			t.Fatalf("account %s has non-positive value %d", a.Name, a.Value)
		}
	}
}
