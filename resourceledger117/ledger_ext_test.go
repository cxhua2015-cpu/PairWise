package resourceledger117

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func opts() Options { return Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100} }

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	l, _ := New(opts())
	bad := []Op{
		{Kind: 0, Name: "a"},
		{Kind: 99, Name: "a"},
		{Kind: Add, Name: ""},
		{Kind: Add, Name: "ABC"},
		{Kind: Add, Name: "a b"},
		{Kind: Add, Name: "a.b"},
		{Kind: Add, Name: "toolongname"},
		{Kind: Delete, Name: "bad!"},
	}
	for _, op := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal(g)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	l2, _ := New(opts())
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, 101}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "a", 0, -101}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "a", 100, 0}, {Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if n := len(l2.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
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
	// Capacity checked only at batch end: delete-then-create within limit succeeds.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "c", 0, 3}}}); e != nil {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l, _ := New(opts())
	r0, e := l.Apply(Batch{})
	if e != nil || r0.Generation != 0 || l.Snapshot().Generation != 0 {
		t.Fatal(e, r0)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if r1.Generation != 1 || r1.Revision != 1 {
		t.Fatal(r1)
	}
	r2, _ := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if r2.Generation != 2 || r2.Revision != 1 {
		t.Fatal(r2)
	}
	r3, _ := l.Apply(Batch{Ops: []Op{{Set, "x", 0, 5}}})
	if r3.Generation != 3 || r3.Revision != 2 || l.Snapshot().NextRevision != 3 {
		t.Fatal(r3, l.Snapshot())
	}
	// Failed batch must not bump generation or revision.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "nope", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := l.Snapshot(); s.Generation != 3 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestChangedAndDeleteInBatch(t *testing.T) {
	l, _ := New(opts())
	r, e := l.Apply(Batch{Ops: []Op{
		{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Set, "b", 0, 7},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Changed[0].Value != 7 {
		t.Fatal(r.Changed)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(opts())
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(2)
	if e != nil || len(top) != 2 || top[0].Name != "c" || top[1].Name != "a" {
		t.Fatal(e, top)
	}
	if _, e = l.Top(0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	top[0].Value = -999
	if l.Snapshot().Accounts[2].Value != 9 {
		t.Fatal("returned slice aliases internal state")
	}
	s := l.Snapshot()
	for i := 1; i < len(s.Accounts); i++ {
		if s.Accounts[i-1].Name >= s.Accounts[i].Name {
			t.Fatal("snapshot not sorted by name")
		}
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
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
			_, _ = l.Apply(Batch{Ops: []Op{{Delete, k, 0, 0}, {Set, k, 0, int64(i)}}})
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 16 {
		t.Fatal(len(s.Accounts))
	}
	for i, a := range s.Accounts {
		if a.Name != string(rune('a'+i)) || a.Value != int64(i) {
			t.Fatal(i, a)
		}
	}
}
