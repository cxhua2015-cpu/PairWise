package resourceledger107

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
	for _, name := range []string{"", "A", "a b", "a.b", "中文", "toolongname", "a/b"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, e)
		}
	}
	for _, name := range []string{"a", "z-0_9", "12345678"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", name, e)
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

func TestOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e) // abs(MinInt64) exceeds any representable bound
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -5}, {Add, "a", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// State unchanged after failed batches.
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatal(got)
	}
}

func TestAbsLimitAndCapacity(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 10}, {Set, "b", 0, -10}}}); e != nil {
		t.Fatal(e)
	}
	// Final capacity checked only at batch end: 3 accounts mid-batch is fine
	// only if it ends at <= 2; here it ends at 3 -> ErrCapacity and rollback.
	_, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 1}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(l.Snapshot().Accounts) != 2 {
		t.Fatal("capacity failure must roll back")
	}
	// Delete-then-create within one batch stays within capacity.
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "c", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r0, e := l.Apply(Batch{Ops: nil})
	if e != nil || r0.Generation != 0 {
		t.Fatal(r0, e)
	}
	s0 := l.Snapshot()
	if s0.Generation != 0 || s0.NextRevision != 1 {
		t.Fatal(s0)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if r1.Generation != 2 || r1.Revision != 3 {
		t.Fatal(r1)
	}
	// Failed batch must not bump generation or revision.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "nope", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
	// Empty batch succeeds without changing generation.
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 2 || r.Revision != 3 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
}

func TestChangedDeduplicates(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{
		{Add, "a", 1, 0}, {Set, "b", 0, 2}, {Add, "a", 3, 0}, {Delete, "b", 0, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	want := []Account{{Name: "a", Value: 4, Revision: 3}}
	if !reflect.DeepEqual(x.Changed, want) {
		t.Fatalf("got %+v want %+v", x.Changed, want)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	names := []string{top[0].Name, top[1].Name, top[2].Name}
	if !reflect.DeepEqual(names, []string{"c", "a", "b"}) {
		t.Fatal(names)
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "d" {
		t.Fatal(all)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	zero, e := l.Top(0)
	if e != nil || len(zero) != 0 {
		t.Fatal(zero, e)
	}
}

func TestReturnedSlicesAreIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	top, _ := l.Top(1)
	top[0].Value = 88
	if got := l.Snapshot().Accounts[0].Value; got != 1 {
		t.Fatal(got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, -1, 0}, {Add, k, 1, 0}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 32 {
		t.Fatal(len(s.Accounts))
	}
	// 32 goroutines * 50 iterations * net +1 per iteration.
	for _, a := range s.Accounts {
		if a.Value != 50 {
			t.Fatalf("%s=%d", a.Name, a.Value)
		}
	}
	// Each iteration allocates 3 revisions (1 + 2) across 2 generations.
	if s.Generation != 32*100 || s.NextRevision != 32*150+1 {
		t.Fatal(s.Generation, s.NextRevision)
	}
}
