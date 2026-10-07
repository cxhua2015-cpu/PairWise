package balanceledger422

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal("expected overflow ErrValue", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatal("expected MinInt64 ErrValue", err)
	}
	l2, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, err := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 11}}}); !errors.Is(err, ErrValue) {
		t.Fatal("expected abs limit ErrValue", err)
	}
	if got := l2.Snapshot().Accounts; len(got) != 0 {
		t.Fatal("failed batch leaked state", got)
	}
}

func TestStructuralValidation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 4, MaxAbsValue: 10})
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Set, "", 0, 1}}},
		{Ops: []Op{{Set, "ABC", 0, 1}}},
		{Ops: []Op{{Set, "toolong", 0, 1}}},
		{Ops: []Op{{Set, "a b", 0, 1}}},
		{Ops: []Op{{Add, "a", 0, 0}}},
	}
	for i, b := range bad {
		if err := l.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: got %v", i, err)
		}
		if _, err := l.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: got %v", i, err)
		}
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Set, "a-z0", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 10})
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || l.Snapshot().Generation != 0 {
		t.Fatal("empty batch must not bump generation", r, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); err != nil {
		t.Fatal(err)
	}
	if g := l.Snapshot().Generation; g != 1 {
		t.Fatal("generation must bump once per non-empty batch", g)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "nope", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if g := l.Snapshot().Generation; g != 1 {
		t.Fatal("failed batch must not bump generation", g)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1}}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, err)
	}
	if n, _ := l.Top(0); n != nil {
		t.Fatal("non-positive n must yield nil", n)
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
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
			_, _, _, _ = l.Preview(Batch{Ops: []Op{{Add, k, 1, 0}}})
			_, _ = l.Top(4)
			_ = l.Snapshot()
			_ = l.Stats()
			_, _ = l.Clone()
			_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
		}()
	}
	w.Wait()
	s := l.Stats()
	if s.Accounts != 32 || s.NextRevision != 33 || s.Generation != 32 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestPreviewClockIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	before := l.Stats()
	if _, _, _, err := l.Preview(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	if got := l.Stats(); got != before {
		t.Fatal("preview advanced clocks", before, got)
	}
}
