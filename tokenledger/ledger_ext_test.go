package tokenledger

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
	for _, n := range []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"} {
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
	// Structural validation happens before any state read: unknown kind wins
	// even when an earlier op would fail against state.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}, {Kind(0), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	r, e = l.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
	if g := l.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "b", 1, 0}, {Set, "c", 0, 2}}})
	if r1.Generation != 1 || r1.Revision != 3 {
		t.Fatal(r1)
	}
	r2, _ := l.Apply(Batch{Ops: []Op{{Delete, "c", 0, 0}}})
	if r2.Generation != 2 || r2.Revision != 3 || len(r2.Changed) != 0 {
		t.Fatal(r2)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
	// Failed batch must not bump generation or revision.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "nope", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s2 := l.Snapshot(); s2.Generation != 2 || s2.NextRevision != 4 {
		t.Fatal(s2)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// MinInt64 itself exceeds any positive absolute-value limit.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed overflow rolls back: a is still MaxInt64.
	top, _ := l.Top(1)
	if top[0].Name != "a" || top[0].Value != math.MaxInt64 {
		t.Fatal(top)
	}
}

func TestAbsLimit(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -20}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t) // MaxAccounts 4
	ops := []Op{{Set, "a", 0, 1}, {Set, "b", 0, 1}, {Set, "c", 0, 1}, {Set, "d", 0, 1}}
	if _, e := l.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	// Transient overflow is fine if a later delete brings the count back.
	ok := []Op{{Set, "e", 0, 1}, {Delete, "a", 0, 0}}
	if _, e := l.Apply(Batch{Ops: ok}); e != nil {
		t.Fatal(e)
	}
	// Net growth beyond capacity fails and rolls back entirely.
	before := l.Snapshot()
	bad := []Op{{Set, "f", 0, 1}, {Set, "g", 0, 1}}
	if _, e := l.Apply(Batch{Ops: bad}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestTopOrder(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -3},
	}})
	top, e := l.Top(4)
	if e != nil {
		t.Fatal(e)
	}
	names := []string{top[0].Name, top[1].Name, top[2].Name, top[3].Name}
	if !reflect.DeepEqual(names, []string{"c", "a", "b", "d"}) {
		t.Fatal(names)
	}
	if n, _ := l.Top(2); len(n) != 2 || n[0].Name != "c" {
		t.Fatal(n)
	}
	if n, _ := l.Top(99); len(n) != 4 {
		t.Fatal(len(n))
	}
	if n, _ := l.Top(0); len(n) != 0 {
		t.Fatal(n)
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "z", 0, 1}, {Set, "m", 0, 2}, {Set, "a", 0, 3}}})
	s := l.Snapshot()
	if s.Accounts[0].Name != "a" || s.Accounts[1].Name != "m" || s.Accounts[2].Name != "z" {
		t.Fatal(s.Accounts)
	}
	// Mutating returned slices must not affect internal state.
	s.Accounts[0].Value = -99
	top, _ := l.Top(1)
	top[0].Value = -99
	s2 := l.Snapshot()
	for _, a := range s2.Accounts {
		if a.Value < 0 {
			t.Fatal("internal state leaked into returned slice")
		}
	}
}

func TestChangedOrderAndFinalValue(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{
		{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Add, "a", 4, 0},
	}})
	if e != nil || len(r.Changed) != 2 {
		t.Fatal(r, e)
	}
	if r.Changed[0].Name != "a" || r.Changed[0].Value != 5 || r.Changed[0].Revision != 3 {
		t.Fatal(r.Changed[0])
	}
	if r.Changed[1].Name != "b" || r.Changed[1].Revision != 2 {
		t.Fatal(r.Changed[1])
	}
	// Create-then-delete within one batch: account absent from Changed and state.
	r2, e := l.Apply(Batch{Ops: []Op{{Set, "tmp", 0, 1}, {Delete, "tmp", 0, 0}}})
	if e != nil || len(r2.Changed) != 0 {
		t.Fatal(r2, e)
	}
	for _, a := range l.Snapshot().Accounts {
		if a.Name == "tmp" {
			t.Fatal("tmp leaked into state")
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
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, -1, 0}, {Add, k, 1, 0}}})
				_, _ = l.Top(8)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	var sum int64
	for _, a := range s.Accounts {
		sum += a.Value
	}
	// Each goroutine nets +1 per iteration (second batch nets 0).
	if sum != 32*50 {
		t.Fatal(sum)
	}
	if s.Generation != 32*50*2 {
		t.Fatal(s.Generation)
	}
}
