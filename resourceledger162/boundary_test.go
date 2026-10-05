package resourceledger162

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestOptionsValidation(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {-1, 8, 10}, {4, 0, 10}, {4, -1, 10}, {4, 8, 0}, {4, 8, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
	if _, e := New(Options{1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongname", "a.b"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Add, n, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
	for _, n := range []string{"a", "0", "a-b_c", "12345678"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Add, n, 1, 0}}}); e != nil {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(99), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	l := led(t)
	// First op is valid and would succeed, second is structurally invalid:
	// nothing may be applied and no revision/generation consumed.
	_, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Kind(7), "b", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != 0 || len(s.Accounts) != 0 || s.NextRevision != 1 {
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed overflow must not consume revision or generation.
	if s := l.Snapshot(); s.Generation != 2 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestAbsLimit(t *testing.T) {
	l := led(t) // MaxAbsValue 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Add, "a", -40, 0}}}); e != nil {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != -20 {
		t.Fatal(got)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); e != nil {
		t.Fatal(e)
	}
	b := l.Snapshot()
	// Interleaved delete frees capacity mid-batch, but final count exceeds.
	_, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Set, "d", 0, 4}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, l.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestDeleteAndRecreate(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 5}, {Delete, "a", 0, 0}, {Add, "a", 2, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(x.Changed) != 1 || x.Changed[0].Value != 2 || x.Changed[0].Revision != 2 {
		t.Fatal(x.Changed)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if len(l.Snapshot().Accounts) != 0 {
		t.Fatal("account not deleted")
	}
}

func TestEmptyBatch(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 {
		t.Fatal(x, e)
	}
	_, _ = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	x, e = l.Apply(Batch{Ops: nil})
	if e != nil || x.Generation != 1 || x.Revision != 1 {
		t.Fatal(x, e)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil {
		t.Fatal(e)
	}
	names := []string{top[0].Name, top[1].Name, top[2].Name}
	if !reflect.DeepEqual(names, []string{"c", "a", "b"}) {
		t.Fatal(names)
	}
	if n, _ := l.Top(100); len(n) != 4 {
		t.Fatal(len(n))
	}
	if n, _ := l.Top(0); len(n) != 0 {
		t.Fatal(n)
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	top, _ := l.Top(2)
	top[0].Value = 999
	if l.Snapshot().Accounts[0].Value == 999 {
		t.Fatal("returned slices alias internal state")
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
	var total int64
	for _, a := range s.Accounts {
		total += a.Value
	}
	if total != 32*50 {
		t.Fatal(total)
	}
	if s.Generation != 32*50 || s.NextRevision != 32*50+1 {
		t.Fatal(s.Generation, s.NextRevision)
	}
}
