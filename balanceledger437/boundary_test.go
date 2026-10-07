package balanceledger437

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
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameAndKindValidation(t *testing.T) {
	l := led(t)
	bad := []Op{
		{Kind: 0, Name: "a", Delta: 1},
		{Kind: 99, Name: "a", Delta: 1},
		{Kind: Add, Name: "", Delta: 1},
		{Kind: Add, Name: "A", Delta: 1},
		{Kind: Add, Name: "a b", Delta: 1},
		{Kind: Add, Name: "a/b", Delta: 1},
		{Kind: Add, Name: "é", Delta: 1},
		{Kind: Add, Name: "abcdefghi", Delta: 1}, // 9 > MaxNameBytes 8
		{Kind: Add, Name: "a", Delta: 0},
	}
	for _, op := range bad {
		b := Batch{Ops: []Op{op}}
		if err := l.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
		if _, err := l.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply op %+v: %v", op, err)
		}
	}
	good := Batch{Ops: []Op{{Kind: Add, Name: "a-z_09", Delta: 1}}}
	if err := l.ValidateBatch(good); err != nil {
		t.Fatal(err)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Kind: Add, Name: "a", Delta: 1}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("overflow add: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "b", Value: math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("min int64 set: %v", err)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MaxInt64 {
		t.Fatalf("state mutated by failed batch: %d", got)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 1}, {Kind: Set, Name: "b", Value: 2}}}); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	// Net-zero intermediate churn still ends over capacity.
	_, err := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "c", Value: 3}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("failed batch leaked state")
	}
	// Delete-then-create stays within final capacity.
	if _, err := l.Apply(Batch{Ops: []Op{{Kind: Delete, Name: "a", Value: 0}, {Kind: Set, Name: "c", Value: 3}}}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 1}}}); err != nil {
		t.Fatal(err)
	}
	r, err = l.Apply(Batch{Ops: []Op{{Kind: Delete, Name: "a"}}})
	if err != nil || r.Generation != 2 || len(r.Changed) != 0 {
		t.Fatalf("delete batch: %+v %v", r, err)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 2 {
		t.Fatalf("snapshot: %+v", s)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Kind: Set, Name: "b", Value: 5},
		{Kind: Set, Name: "a", Value: 5},
		{Kind: Set, Name: "c", Value: 9},
		{Kind: Set, Name: "d", Value: -1},
	}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 {
		t.Fatal(top, err)
	}
	want := []string{"c", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatalf("top[%d]=%s want %s", i, top[i].Name, w)
		}
	}
	top[0].Value = -999
	if again, _ := l.Top(1); again[0].Value != 9 {
		t.Fatal("returned slice aliases internal state")
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative top: %v", err)
	}
	if got, _ := l.Top(0); len(got) != 0 {
		t.Fatal("top(0) should be empty")
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 1}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Kind: Set, Name: "b", Value: 2}}})
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 2 {
		t.Fatal("clone and original diverge incorrectly")
	}
}

func TestPreviewErrorParity(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 1}}})
	batches := []Batch{
		{Ops: []Op{{Kind: Delete, Name: "nope"}}},
		{Ops: []Op{{Kind: Add, Name: "a", Delta: 100}}},
		{Ops: []Op{{Kind: Set, Name: "x", Value: 1}, {Kind: Set, Name: "y", Value: 2}, {Kind: Set, Name: "z", Value: 3}, {Kind: Set, Name: "w", Value: 4}}},
		{Ops: []Op{{Kind: 42, Name: "a"}}},
	}
	for _, b := range batches {
		_, applyErr := l.Apply(b)
		r, s, st, prevErr := l.Preview(b)
		if applyErr != prevErr {
			t.Fatalf("batch %+v: apply=%v preview=%v", b, applyErr, prevErr)
		}
		if prevErr != nil && (!reflect.DeepEqual(r, Result{}) || !reflect.DeepEqual(s, Snapshot{}) || !reflect.DeepEqual(st, Stats{})) {
			t.Fatalf("failed preview returned non-zero values for %+v", b)
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
			k := string(rune('a'+i%26)) + string(rune('a'+(i/26)))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Kind: Add, Name: k, Delta: 1}}})
				_, _, _, _ = l.Preview(Batch{Ops: []Op{{Kind: Add, Name: k, Delta: 1}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
				_ = l.Stats()
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	if st.Accounts != 32 {
		t.Fatalf("accounts=%d", st.Accounts)
	}
	var total int64
	for _, a := range l.Snapshot().Accounts {
		total += a.Value
	}
	if total != 32*50 {
		t.Fatalf("total=%d", total)
	}
}
