package resourceledger172

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, 1, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, n := range []string{"", "A", "a b", "a/b", "toolongname", "é"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(n, e)
		}
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "ok_nm-1", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}, {Delete, "z", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if got := l.Snapshot(); got.Generation != 0 || len(got.Accounts) != 0 {
		t.Fatal(got)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 5}, {Add, "a", -10, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Accounts[0].Value != math.MaxInt64 || s.Generation != 1 {
		t.Fatal(s)
	}

	l2 := led(t)
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "a", -21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	l := led(t)
	r0, e := l.Apply(Batch{})
	if e != nil || r0.Generation != 0 {
		t.Fatal(r0, e)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if r1.Generation != 1 || r1.Revision != 2 || len(r1.Changed) != 1 || r1.Changed[0].Name != "b" {
		t.Fatal(r1)
	}
	s := l.Snapshot()
	if s.NextRevision != 3 || s.Generation != 1 {
		t.Fatal(s)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if l.Snapshot().Generation != 1 {
		t.Fatal("generation changed on failure")
	}
}

func TestTopOrderAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1}}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	top[0].Value = -999
	if l.Snapshot().Accounts[2].Name != "c" || l.Snapshot().Accounts[2].Value != 9 {
		t.Fatal("snapshot aliased")
	}
	s := l.Snapshot()
	s.Accounts[0].Value = 123
	if l.Snapshot().Accounts[0].Value == 123 {
		t.Fatal("snapshot not isolated")
	}
	names := []string{}
	for _, a := range l.Snapshot().Accounts {
		names = append(names, a.Name)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatal(names)
		}
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
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Set, k, 0, int64(j)}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 32 || s.Generation != 32*100 {
		t.Fatal(len(s.Accounts), s.Generation)
	}
}
