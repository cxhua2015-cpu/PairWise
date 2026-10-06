package balanceledger282

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
		{Ops: []Op{{Add, "Bad", 1, 0}}},
		{Ops: []Op{{Add, "toolongname", 1, 0}}},
		{Ops: []Op{{Add, "a b", 1, 0}}},
		{Ops: []Op{{Add, "a", 0, 0}}},
		{Ops: []Op{{Add, "a", 21, 0}}},
		{Ops: []Op{{Add, "a", -21, 0}}},
		{Ops: []Op{{Set, "a", 0, 21}}},
		{Ops: []Op{{Set, "a", 0, -21}}},
	}
	for i, b := range cases {
		if err := l.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := l.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	ok := Batch{Ops: []Op{{Add, "a-b_1", 20, 0}, {Set, "c", 0, -20}, {Delete, "c", 0, 0}}}
	if err := l.ValidateBatch(ok); err != nil {
		t.Fatal(err)
	}
}

func TestOverflowGuards(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	l2, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l2.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	_, err := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Set, "d", 0, 4}, {Delete, "a", 0, 0}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if s := l.Snapshot(); s.Generation != before.Generation || len(s.Accounts) != 2 {
		t.Fatalf("state mutated: %+v", s)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	r, err = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
	r, err = l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if err != nil || r.Generation != 2 || r.Revision != 1 {
		t.Fatal(r, err)
	}
	if z := l.Stats(); z.Generation != 2 || z.NextRevision != 2 || z.Accounts != 0 {
		t.Fatalf("%+v", z)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -3},
	}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 {
		t.Fatal(top, err)
	}
	want := []string{"c", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatalf("top[%d]=%s want %s", i, top[i].Name, w)
		}
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(len(all))
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r.Changed[0].Value = 999
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	top, _ := l.Top(1)
	top[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("returned slices alias internal state")
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 7}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Generation != 1 || c.Stats().NextRevision != 2 {
		t.Fatal("clone lost logical clocks")
	}
	_, _ = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	if c.Snapshot().Accounts[0].Value != 7 {
		t.Fatal("clone affected by original")
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
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, name, 0, 1}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				if j%17 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	wg.Wait()
	z := l.Stats()
	if z.Accounts != 32 || int(z.NextRevision)-1 != 32*50 {
		t.Fatalf("%+v", z)
	}
}
