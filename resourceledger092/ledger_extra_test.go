package resourceledger092

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{MaxAccounts: 0, MaxNameBytes: 8, MaxAbsValue: 10},
		{MaxAccounts: 4, MaxNameBytes: 0, MaxAbsValue: 10},
		{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 0},
		{MaxAccounts: -1, MaxNameBytes: 8, MaxAbsValue: 10},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, name := range []string{"", "A", "a b", "a/b", "toolongname", "中文"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(name, e)
		}
	}
	for _, name := range []string{"a", "a-b_c", "0", "abcdefgh"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); e != nil {
			t.Fatal(name, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	b := l.Snapshot()
	// Bad op appears after a state-mutating op: nothing may change.
	_, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}, {Kind: 7, Name: "c"}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(b, l.Snapshot()) {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -2, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MinInt64+1 {
		t.Fatal(got)
	}
}

func TestAbsLimit(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 20, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -40, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}, {Set, "d", 0, 4}}})
	b := l.Snapshot()
	_, e := l.Apply(Batch{Ops: []Op{{Set, "e", 0, 5}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, l.Snapshot()) {
		t.Fatal(e)
	}
	// Delete then create within the same batch stays within capacity.
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "e", 0, 5}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if l.Snapshot().Generation != 0 {
		t.Fatal("generation must not change on empty batch")
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "b", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	_, _ = l.Apply(Batch{Ops: []Op{{Delete, "z", 0, 0}}})
	r2, _ := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 1}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
}

func TestChangedAccounts(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "a", 2, 0}, {Set, "b", 0, 5}, {Delete, "b", 0, 0}}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Name != "a" || r.Changed[0].Value != 3 || r.Changed[0].Revision != 2 {
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
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "d" {
		t.Fatal(all)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if zero, e := l.Top(0); e != nil || len(zero) != 0 {
		t.Fatal(zero, e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "z", 0, 1}, {Set, "m", 0, 2}, {Set, "a", 0, 3}}})
	s := l.Snapshot()
	if s.Accounts[0].Name != "a" || s.Accounts[1].Name != "m" || s.Accounts[2].Name != "z" {
		t.Fatal(s)
	}
	s.Accounts[0].Value = 999
	top, _ := l.Top(1)
	top[0].Value = -999
	if l.Snapshot().Accounts[0].Value != 3 {
		t.Fatal("returned slices must be isolated from internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 16 || s.Generation != 800 {
		t.Fatal(len(s.Accounts), s.Generation)
	}
	for _, a := range s.Accounts {
		if a.Value != 50 {
			t.Fatal(a)
		}
	}
}
