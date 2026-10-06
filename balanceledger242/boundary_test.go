package balanceledger242

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestOverflowGuards(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("overflow add: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("minint delta: %v", err)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatalf("state mutated on failure: %d", got)
	}
}

func TestAbsLimitEnforced(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 10})
	if err := l.ValidateBatch(Batch{Ops: []Op{{Set, "a", 0, 11}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("set over limit: %v", err)
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "a", -11, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("delta over limit: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 9}, {Add, "a", 2, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("result over limit: %v", err)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatalf("rollback failed, accounts=%d", n)
	}
}

func TestStructuralValidation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 4, MaxAbsValue: 10})
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Add, "", 1, 0}}},
		{Ops: []Op{{Add, "ABC", 1, 0}}},
		{Ops: []Op{{Add, "toolongname", 1, 0}}},
		{Ops: []Op{{Add, "a", 0, 0}}},
		{Ops: []Op{{Add, "a", 1, 1}}},
		{Ops: []Op{{Set, "a", 1, 1}}},
		{Ops: []Op{{Delete, "a", 1, 0}}},
	}
	for i, b := range cases {
		if err := l.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "a-z0", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	_, _ = l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}}})
	if g := l.Stats().Generation; g != 1 {
		t.Fatalf("failed batch bumped generation: %d", g)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}}})
	top, err := l.Top(2)
	if err != nil || len(top) != 2 || top[0].Name != "c" || top[1].Name != "a" {
		t.Fatal(top, err)
	}
	top[0].Value = -999
	if l.Snapshot().Accounts[2].Value != 9 {
		t.Fatal("returned slice aliases internal state")
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {1, 1, -5}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, err)
		}
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i/4)) + string(rune('a'+i%4))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				if j%25 == 0 {
					c, err := l.Clone()
					if err == nil {
						_, _ = c.Apply(Batch{Ops: []Op{{Set, "tmp", 0, 1}}})
					}
				}
			}
		}()
	}
	w.Wait()
	s := l.Stats()
	if s.Accounts != 16 || s.Generation != 16*50 {
		t.Fatalf("%+v", s)
	}
	if uint64(s.NextRevision) != 1+16*50 {
		t.Fatalf("revisions not contiguous: %+v", s)
	}
}
