package balanceledger322

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
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 16, MaxNameBytes: 8, MaxAbsValue: 20})
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "0", "-", "_", "a-b_c9", "12345678"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(99), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", math.MaxInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// MinInt64 delta lands out of the absolute-value bound.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 0}, {Add, "a", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestAbsLimit(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}}}); e != nil {
		t.Fatal(e)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || l.Snapshot().Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 2}, {Set, "a", 0, 3}}})
	if r.Generation != 2 || r.Revision != 3 || len(r.Changed) != 1 || r.Changed[0].Value != 3 {
		t.Fatal(r)
	}
	if s := l.Snapshot(); s.NextRevision != 4 || s.Generation != 2 {
		t.Fatal(s)
	}
	// Failed batch must not bump generation or revision.
	_, _ = l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}})
	if s := l.Snapshot(); s.NextRevision != 4 || s.Generation != 2 {
		t.Fatal(s)
	}
}

func TestDeleteMissingAndReadd(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 5}, {Delete, "a", 0, 0}, {Add, "a", 2, 0}}}); e != nil {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != 2 {
		t.Fatal(s)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
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
	if top, _ = l.Top(100); len(top) != 4 {
		t.Fatal(top)
	}
	// Mutating the returned slice must not affect internal state.
	top[0].Value = -999
	if again, _ := l.Top(1); again[0].Value != 9 {
		t.Fatal(again)
	}
	s := l.Snapshot()
	s.Accounts[0].Value = -999
	if again, _ := l.Top(1); again[0].Value != 9 {
		t.Fatal(again)
	}
}

func TestSnapshotSorted(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "c", 0, 1}, {Set, "a", 0, 1}, {Set, "b", 0, 1}}})
	s := l.Snapshot()
	for i := 1; i < len(s.Accounts); i++ {
		if s.Accounts[i-1].Name >= s.Accounts[i].Name {
			t.Fatal(s.Accounts)
		}
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
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Set, k, 0, int64(j)}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
			}
			_, _ = l.Apply(Batch{Ops: []Op{{Delete, k, 0, 0}}})
		}()
	}
	w.Wait()
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
}
