package balanceledger337

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, name := range []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"} {
		_, e := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, e)
		}
	}
	for _, name := range []string{"a", "z0-_", "12345678"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", name, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	for _, k := range []Kind{0, 4, 255} {
		if _, e := l.Apply(Batch{Ops: []Op{{k, "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: %v", k, e)
		}
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
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "c", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Accounts[0].Value != math.MaxInt64 || len(s.Accounts) != 1 {
		t.Fatal(s)
	}
	l2, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 20})
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 15}, {Add, "a", 10, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 10}, {Add, "a", -20, 0}}}); e != nil {
		t.Fatal(e)
	}
	if got := l2.Snapshot().Accounts[0].Value; got != -10 {
		t.Fatal(got)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	l := led(t)
	r0, e := l.Apply(Batch{})
	if e != nil || r0.Generation != 0 || r0.Revision != 0 || len(r0.Changed) != 0 {
		t.Fatal(r0, e)
	}
	if l.Snapshot().Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Delete, "a", 0, 0}}})
	if r1.Generation != 1 || r1.Revision != 1 || len(r1.Changed) != 0 {
		t.Fatal(r1)
	}
	r2, _ := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}, {Set, "c", 0, 3}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	s := l.Snapshot()
	if s.NextRevision != 4 || s.Generation != 2 {
		t.Fatal(s)
	}
	if s.Accounts[0].Revision != 2 || s.Accounts[1].Revision != 3 {
		t.Fatal(s.Accounts)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if l.Snapshot().Generation != 2 || l.Snapshot().NextRevision != 4 {
		t.Fatal("failed batch mutated state")
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
}

func TestTopOrderingAndLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -1}, {Set, "d", 0, 9},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	want := []string{"d", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(all)
	}
	zero, _ := l.Top(0)
	if len(zero) != 0 {
		t.Fatal(zero)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s1 := l.Snapshot()
	s1.Accounts[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("snapshot aliases internal state")
	}
	top, _ := l.Top(1)
	top[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("top aliases internal state")
	}
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}}})
	r.Changed[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("result aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}, {Add, k, -1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 16 {
		t.Fatal(len(s.Accounts))
	}
	for _, a := range s.Accounts {
		if a.Value != 0 {
			t.Fatal(a)
		}
	}
	if s.NextRevision != 1+16*50*2 {
		t.Fatal(s.NextRevision)
	}
	if s.Generation != 16*50 {
		t.Fatal(s.Generation)
	}
}

func TestChangedContents(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "b", 1, 0}, {Set, "a", 0, 2}, {Add, "b", 1, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	want := []Account{{Name: "a", Value: 2, Revision: 2}, {Name: "b", Value: 2, Revision: 3}}
	if !reflect.DeepEqual(r.Changed, want) {
		t.Fatal(r.Changed)
	}
}
