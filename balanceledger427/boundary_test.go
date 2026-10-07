package balanceledger427

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestBoundaryValidation(t *testing.T) {
	if _, e := New(Options{0, 1, 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{1, 1, 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	l := led(t)
	bad := []Op{
		{Kind(0), "a", 1, 0}, {Kind(9), "a", 1, 0},
		{Add, "", 1, 0}, {Add, "A", 1, 0}, {Add, "a b", 1, 0},
		{Add, "toolongname", 1, 0}, {Add, "a", 0, 0},
	}
	for _, op := range bad {
		if e := l.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if e := l.ValidateBatch(Batch{Ops: []Op{{Add, "ok-1_", 1, 0}, {Set, "b", 0, -3}}}); e != nil {
		t.Fatal(e)
	}
	if e := l.ValidateBatch(Batch{}); e != nil {
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
	small, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 5})
	if _, e := small.Apply(Batch{Ops: []Op{{Set, "a", 0, 6}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := small.Apply(Batch{Ops: []Op{{Add, "a", -6, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if len(small.Snapshot().Accounts) != 0 {
		t.Fatal("failed batch leaked state")
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	if g := l.Stats().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	_, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}, {Delete, "c", 0, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(l.Snapshot().Accounts) != 2 {
		t.Fatal("capacity failure leaked state")
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r.Changed[0].Value = 99
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("returned slices alias internal state")
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
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
			_, _, _, _ = l.Preview(Batch{Ops: []Op{{Add, k, 1, 0}}})
			_, _ = l.Clone()
			_ = l.Stats()
			_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
			_, _ = l.Top(4)
			_ = l.Snapshot()
		}()
	}
	w.Wait()
	st := l.Stats()
	if st.Accounts != 32 || st.Generation != 32 || st.NextRevision != 33 {
		t.Fatalf("%+v", st)
	}
}

func TestPreviewErrorParity(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 5})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	batches := []Batch{
		{Ops: []Op{{Delete, "zz", 0, 0}}},
		{Ops: []Op{{Set, "b", 0, 1}, {Set, "c", 0, 1}}},
		{Ops: []Op{{Add, "a", 100, 0}}},
		{Ops: []Op{{Kind(7), "a", 0, 0}}},
	}
	for _, b := range batches {
		_, _, _, pe := l.Preview(b)
		c, _ := l.Clone()
		_, ae := c.Apply(b)
		if pe != ae {
			t.Fatalf("batch %+v: preview=%v apply=%v", b, pe, ae)
		}
	}
}
