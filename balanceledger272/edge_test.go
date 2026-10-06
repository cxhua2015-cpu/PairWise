package balanceledger272

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {-1, 8, 10}, {4, 8, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, n := range []string{"", "A", "a b", "a.b", "toolongname", "汉"} {
		if e := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z0-_", "12345678"} {
		if e := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("%q: %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	if e := l.ValidateBatch(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestOverflowGuards(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -2, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Delta beyond the absolute limit is rejected before arithmetic.
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "c", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestAbsLimitAndZeroDelta(t *testing.T) {
	l := led(t)
	if e := l.ValidateBatch(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Add, "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Delete, "x", 0, 0}}})
	if r.Generation != 0 { // failed batch: no generation bump
		t.Fatal(r)
	}
	r, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	if s := l.Stats(); s.Generation != 1 || s.NextRevision != 3 || s.Accounts != 2 {
		t.Fatal(s)
	}
}

func TestChangedDeduplicates(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "a", 1, 0}, {Set, "a", 0, 7}}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Value != 7 || r.Changed[0].Revision != 3 {
		t.Fatal(r, e)
	}
	// Created then deleted in one batch: absent from Changed.
	r, e = l.Apply(Batch{Ops: []Op{{Set, "tmp", 0, 1}, {Delete, "tmp", 0, 0}}})
	if e != nil || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if top, _ = l.Top(99); len(top) != 4 {
		t.Fatal(top)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("snapshot aliases state")
	}
	top, _ := l.Top(1)
	top[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("top aliases state")
	}
}

func TestCloneClocksAndIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	c, e := l.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clocks not preserved")
	}
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}}})
	if len(c.Snapshot().Accounts) != 1 {
		t.Fatal("clone affected by original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				if j == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	if st.Accounts != 32 || st.NextRevision != 32*50+1 {
		t.Fatal(st)
	}
}
