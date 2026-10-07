package balanceledger317

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, name := range []string{"", "A", "a b", "a/b", "中文", "toolongname", "a.b"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
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
		if _, e := l.Apply(Batch{Ops: []Op{{k, "a", 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: %v", k, e)
		}
	}
}

func TestEmptyBatch(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if s := l.Snapshot(); s.Generation != 0 || s.NextRevision != 1 {
		t.Fatal(s)
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
}

func TestRevisionSequence(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "a", 1, 0}, {Set, "b", 0, 2}}})
	if e != nil || r.Revision != 3 {
		t.Fatal(r, e)
	}
	if r.Changed[0].Revision != 2 || r.Changed[1].Revision != 3 {
		t.Fatal(r.Changed)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := l.Snapshot(); s.NextRevision != 4 || s.Generation != 1 {
		t.Fatal(s)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Set, "c", 0, 3}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteMissingAndReAdd(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Add, "a", 2, 0}}}); e != nil {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != 2 {
		t.Fatal(s)
	}
}

func TestTopOrder(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "d" {
		t.Fatal(all)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 99
	r, _ := l.Apply(Batch{Ops: []Op{{Add, "b", 1, 0}}})
	r.Changed[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("internal state mutated via returned slice")
	}
}

func TestConcurrentApply(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i%26)) + string(rune('a'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	var total int64
	for _, a := range s.Accounts {
		total += a.Value
	}
	if total != 32*50 {
		t.Fatal(total)
	}
	if s.Generation != 32*50 || s.NextRevision != 32*50+1 {
		t.Fatal(s)
	}
}
