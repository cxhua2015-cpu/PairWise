package balanceledger437

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {1, 1, -1}, {-1, 1, 1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "工具", "toolongname", "a.b"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "ok_nm-1", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(99), "a", 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if len(l.Snapshot().Accounts) != 2 {
		t.Fatal("failed batches leaked state")
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || r.Changed != nil || l.Snapshot().Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if r.Generation != 1 || l.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestRevisionContinuityAndChanged(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if e != nil || r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r, e)
	}
	s := l.Snapshot()
	if s.NextRevision != 3 || len(s.Accounts) != 1 || s.Accounts[0].Revision != 2 {
		t.Fatal(s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1}}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
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
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("returned slices alias internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 25; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _, _, _ = l.Preview(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Clone()
				_ = l.Stats()
				_ = l.Snapshot()
				_, _ = l.Top(4)
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	if st.Accounts != 16 || st.Generation != 400 || st.NextRevision != 401 {
		t.Fatalf("%+v", st)
	}
}

func TestPreviewCapacityParity(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 10})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	b := Batch{Ops: []Op{{Set, "b", 0, 2}}}
	_, ae := l.Apply(b)
	_, _, _, pe := l.Preview(b)
	if !errors.Is(ae, ErrCapacity) || ae != pe {
		t.Fatal(ae, pe)
	}
}
