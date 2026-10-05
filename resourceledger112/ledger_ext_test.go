package resourceledger112

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
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	l := led(t)
	cases := []Op{
		{Kind(0), "a", 0, 0}, {Kind(4), "a", 0, 0}, {Kind(99), "a", 0, 0},
		{Add, "", 1, 0}, {Add, "A", 1, 0}, {Add, "a b", 1, 0},
		{Add, "a.b", 1, 0}, {Add, "é", 1, 0}, {Add, "toolongname", 1, 0},
		{Set, "a/b", 0, 1}, {Delete, "UPPER", 0, 0},
	}
	for _, op := range cases {
		if _, e := l.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	// Structural validation happens before any state read: even a batch whose
	// first op would fail semantically must report ErrInvalidInput.
	_, e := l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}, {Kind(7), "x", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "ok", 1, 0}, {Add, "bad name", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(l.Snapshot().Accounts) != 0 {
		t.Fatal("state mutated by invalid batch")
	}
}

func TestValidNames(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 20})
	for _, n := range []string{"a", "z0-_", "01234567", "-", "_"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed overflow attempts must not change state.
	s := l.Snapshot()
	if len(s.Accounts) != 2 || s.Accounts[0].Value != math.MaxInt64 || s.Accounts[1].Value != math.MinInt64+1 {
		t.Fatal(s)
	}
}

func TestAbsLimitAndRollback(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Intermediate violation inside a batch fails even if a later op would fix it.
	_, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 30}, {Set, "a", 0, 1}}})
	if !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Rollback: generation and revision unchanged.
	s := l.Snapshot()
	if s.Generation != 0 || s.NextRevision != 1 || len(s.Accounts) != 0 {
		t.Fatal(s)
	}
}

func TestCapacityEndOfBatch(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	// Transiently 3 accounts, back to 2 by batch end: allowed.
	_, e := l.Apply(Batch{Ops: []Op{
		{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}, {Delete, "c", 0, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// 3 accounts at batch end: rejected and rolled back.
	_, e = l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(l.Snapshot().Accounts) != 2 {
		t.Fatal("capacity failure leaked state")
	}
}

func TestDeleteMissingAndReAdd(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "ghost", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 5}, {Delete, "a", 0, 0}, {Add, "a", 2, 0}}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Value != 2 || r.Changed[0].Revision != 2 {
		t.Fatal(e, r)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r0, e := l.Apply(Batch{}) // empty batch: no generation bump
	if e != nil || r0.Generation != 0 {
		t.Fatal(e, r0)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	r2, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "a", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 1 || r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r1, r2)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 || s.Accounts[0].Revision != 3 {
		t.Fatal(s)
	}
	// Failed batch leaves generation/revision untouched.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "nope", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(l.Snapshot(), s) {
		t.Fatal("failed batch mutated state")
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3}, {Set, "d", 0, 9},
	}})
	top, e := l.Top(4)
	if e != nil {
		t.Fatal(e)
	}
	names := []string{top[0].Name, top[1].Name, top[2].Name, top[3].Name}
	if !reflect.DeepEqual(names, []string{"d", "a", "b", "c"}) {
		t.Fatal(names)
	}
	if got, _ := l.Top(2); len(got) != 2 || got[0].Name != "d" || got[1].Name != "a" {
		t.Fatal(got)
	}
	if got, _ := l.Top(0); len(got) != 0 {
		t.Fatal(got)
	}
	if got, _ := l.Top(99); len(got) != 4 {
		t.Fatal(got)
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	top, _ := l.Top(1)
	snap := l.Snapshot()
	r, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	top[0].Value = 999
	snap.Accounts[0].Value = 999
	r.Changed[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 2 {
		t.Fatal("returned slices alias internal state")
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
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}, {Add, k, -1, 0}, {Add, k, 1, 0}}})
				_, _ = l.Top(8)
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
	for i, acc := range s.Accounts {
		if acc.Name != string(rune('a'+i)) || acc.Value != int64(i) {
			t.Fatal(s.Accounts)
		}
	}
	// 16 writers x 50 batches x 3 ops + 16 final ops = 2416 revisions.
	if s.NextRevision != 2417 {
		t.Fatal(s.NextRevision)
	}
}
