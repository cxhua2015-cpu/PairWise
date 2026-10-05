package shardbalance

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {1, 1, -5}, {-1, 1, 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, n := range []string{"", "A", "a b", "a/b", "é", "toolongname"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "ok-n_1", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(99), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatal(got)
	}
	l2 := led(t)
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}}}); e != nil {
		t.Fatal(e)
	}
}

func TestCapacityEndOfBatch(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Set, "c", 0, 3}}}); e != nil {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 2 {
		t.Fatal(n)
	}
	_, e := l.Apply(Batch{Ops: []Op{{Set, "d", 0, 1}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	l := led(t)
	if s := l.Snapshot(); s.Generation != 0 || s.NextRevision != 1 {
		t.Fatal(s)
	}
	x, e := l.Apply(Batch{})
	if e != nil || x.Generation != 0 || l.Snapshot().Generation != 0 {
		t.Fatal(e, x)
	}
	x, _ = l.Apply(Batch{Ops: []Op{{Delete, "nope", 0, 0}}})
	if l.Snapshot().Generation != 0 {
		t.Fatal("failed batch bumped generation")
	}
	x, _ = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "a", 1, 0}}})
	if x.Generation != 1 || x.Revision != 2 {
		t.Fatal(x)
	}
	if s := l.Snapshot(); s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestTopOrderAndIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3}}})
	top, e := l.Top(2)
	if e != nil || len(top) != 2 || top[0].Name != "a" || top[1].Name != "b" {
		t.Fatal(e, top)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	top[0].Value = 999
	if l.Snapshot().Accounts[0].Value == 999 {
		t.Fatal("returned slice aliases internal state")
	}
	s := l.Snapshot()
	if s.Accounts[0].Name != "a" || s.Accounts[2].Name != "c" {
		t.Fatal("snapshot not name-sorted", s)
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
				_, _ = l.Top(10)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	var total int64
	for _, a := range l.Snapshot().Accounts {
		total += a.Value
	}
	if total != 32*50 {
		t.Fatal(total)
	}
}
