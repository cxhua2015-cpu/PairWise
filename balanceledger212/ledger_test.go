package balanceledger212

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {-1, 8, 10}, {4, -1, 10}, {4, 8, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 16, MaxNameBytes: 8, MaxAbsValue: 20})
	for _, n := range []string{"", "A", "a b", "a/b", "中文", "toolongname", "a.b"} {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}, {Set, "b", 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if got := len(l.Snapshot().Accounts); got != 0 {
		t.Fatal(got)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -2, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Abs limit still enforced when MaxAbsValue is small.
	l2 := led(t)
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := len(l2.Snapshot().Accounts); got != 0 {
		t.Fatal(got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	// Failed batch must not bump generation or revision.
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	if r.Generation != 2 || r.Revision != 3 {
		t.Fatal(r)
	}
	if s.NextRevision != 3 || l.Snapshot().NextRevision != 4 {
		t.Fatal(l.Snapshot())
	}
}

func TestChangedAndIsolation(t *testing.T) {
	l := led(t)
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Add, "a", 2, 0}, {Set, "b", 0, 5}, {Delete, "b", 0, 0}}})
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[0].Value != 3 || r.Changed[0].Revision != 2 ||
		r.Changed[1].Name != "b" || r.Changed[1].Revision != 3 {
		t.Fatal(r.Changed)
	}
	// Mutating returned slices must not affect internal state.
	r.Changed[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 99
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 3 {
		t.Fatal(l.Snapshot().Accounts[0])
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -1}, {Set, "d", 0, 9},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "d" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if top, _ = l.Top(0); len(top) != 0 {
		t.Fatal(top)
	}
	if top, _ = l.Top(100); len(top) != 4 {
		t.Fatal(top)
	}
}

func TestSnapshotSorted(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "c", 0, 1}, {Set, "a", 0, 2}, {Set, "b", 0, 3}}})
	s := l.Snapshot()
	if s.Accounts[0].Name != "a" || s.Accounts[1].Name != "b" || s.Accounts[2].Name != "c" {
		t.Fatal(s.Accounts)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}})
	// Peak of 3 accounts mid-batch, ends at 3 -> must fail and roll back.
	_, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Delete, "a", 0, 0}, {Set, "d", 0, 4}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 2 || s.Generation != 1 {
		t.Fatal(s)
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
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 32 {
		t.Fatal(len(s.Accounts))
	}
	var sum int64
	for _, a := range s.Accounts {
		sum += a.Value
	}
	if sum != 32*50 {
		t.Fatal(sum)
	}
	if s.NextRevision != 32*50+1 {
		t.Fatal(s.NextRevision)
	}
}
