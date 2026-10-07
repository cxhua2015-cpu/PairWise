package balanceledger307

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongname", "a.b"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-0_", "12345678"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(0), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(99), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatal(got)
	}
}

func TestAbsLimitDelta(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if len(l.Snapshot().Accounts) != 0 {
		t.Fatal("state leaked after failed batches")
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{
		{Set, "a", 0, 1}, {Set, "b", 0, 1}, {Set, "c", 0, 1}, {Set, "d", 0, 1}, {Set, "e", 0, 1},
	}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
}

func TestEmptyBatch(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	s := l.Snapshot()
	if s.Generation != 0 || s.NextRevision != 1 {
		t.Fatal(s)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r1, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r2, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Delete, "a", 0, 0}}})
	if r1.Generation != 1 || r2.Generation != 2 {
		t.Fatal(r1.Generation, r2.Generation)
	}
	if r1.Revision != 1 || r2.Revision != 2 {
		t.Fatal(r1.Revision, r2.Revision)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestChangedDedup(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "a", 1, 0}, {Set, "a", 0, 7}}})
	if e != nil || len(r.Changed) != 1 {
		t.Fatal(e, r.Changed)
	}
	c := r.Changed[0]
	if c.Value != 7 || c.Revision != 3 {
		t.Fatal(c)
	}
}

func TestTopOrder(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -3}}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(e, top)
	}
	if top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top)
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(len(all))
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("snapshot aliases internal state")
	}
	top, _ := l.Top(1)
	top[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("top aliases internal state")
	}
}

func TestConcurrentStress(t *testing.T) {
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
