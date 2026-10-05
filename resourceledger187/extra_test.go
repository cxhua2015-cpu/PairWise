package resourceledger187

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
			t.Fatal(o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, name := range []string{"", "A", "a b", "a.b", "中文", "toolongname", "a/b"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, e)
		}
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "ok-nm_1", 0, 1}}}); e != nil {
		t.Fatal(e)
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

func TestOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}, {Add, "a", -5, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed batch must roll back.
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatal(got)
	}
}

func TestAbsLimitOnResult(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", -40, 0}}}); e != nil {
		t.Fatal(e) // intermediate -20 ok
	}
	if got := l.Snapshot().Accounts[0].Value; got != -20 {
		t.Fatal(got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{}) // empty batch: no generation bump
	if e != nil || r.Generation != 0 || l.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "b", 1, 0}, {Delete, "b", 0, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 || len(s.Accounts) != 1 {
		t.Fatal(s)
	}
	// Delete of missing account fails and rolls back.
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if l.Snapshot().Generation != 1 || l.Snapshot().NextRevision != 3 {
		t.Fatal(l.Snapshot())
	}
}

func TestChangedDedup(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "a", 1, 0}, {Set, "a", 0, 7}}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Value != 7 || r.Changed[0].Revision != 3 {
		t.Fatal(e, r)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(e, top)
	}
	want := []string{"c", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if _, e = l.Top(0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Returned slice is isolated from internal state.
	top[0].Value = -999
	again, _ := l.Top(1)
	if again[0].Value != 9 {
		t.Fatal(again)
	}
}

func TestSnapshotSorted(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "z", 0, 1}, {Set, "m", 0, 1}, {Set, "a", 0, 1}}})
	s := l.Snapshot()
	if s.Accounts[0].Name != "a" || s.Accounts[1].Name != "m" || s.Accounts[2].Name != "z" {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 1}}})
	before := l.Snapshot()
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 1}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := l.Snapshot(); got.Generation != before.Generation || len(got.Accounts) != 2 {
		t.Fatal(got)
	}
}

func TestConcurrentStress(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}, {Add, k, -1, 0}, {Add, k, 1, 0}}})
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
		if a.Value != 50 {
			t.Fatal(a)
		}
	}
	// Revisions are consecutive with no gaps or duplicates.
	if s.NextRevision != 1+16*50*3 {
		t.Fatal(s.NextRevision)
	}
}
