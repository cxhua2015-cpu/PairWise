package balanceledger432

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestBoundaryValidation(t *testing.T) {
	l := led(t)
	cases := []struct {
		name string
		op   Op
		want error
	}{
		{"unknown kind", Op{Kind: 9, Name: "a"}, ErrInvalidInput},
		{"empty name", Op{Kind: Set, Name: ""}, ErrInvalidInput},
		{"uppercase name", Op{Kind: Set, Name: "Ab"}, ErrInvalidInput},
		{"space name", Op{Kind: Set, Name: "a b"}, ErrInvalidInput},
		{"too long name", Op{Kind: Set, Name: "123456789"}, ErrInvalidInput},
		{"zero delta", Op{Kind: Add, Name: "a", Delta: 0}, ErrInvalidInput},
		{"delta over limit", Op{Kind: Add, Name: "a", Delta: 21}, ErrValue},
		{"set over limit", Op{Kind: Set, Name: "a", Value: -21}, ErrValue},
		{"min int64 delta", Op{Kind: Add, Name: "a", Delta: math.MinInt64}, ErrValue},
		{"min int64 set", Op{Kind: Set, Name: "a", Value: math.MinInt64}, ErrValue},
	}
	for _, c := range cases {
		if err := l.ValidateBatch(Batch{Ops: []Op{c.op}}); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v want %v", c.name, err, c.want)
		}
		if _, err := l.Apply(Batch{Ops: []Op{c.op}}); !errors.Is(err, c.want) {
			t.Errorf("apply %s: got %v want %v", c.name, err, c.want)
		}
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatalf("failed batches changed generation: %d", g)
	}
}

func TestOverflowArithmetic(t *testing.T) {
	l, err := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("expected overflow ErrValue, got %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("expected underflow ErrValue, got %v", err)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	r, err = l.Apply(Batch{})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatalf("empty batch after write: %+v %v", r, err)
	}
	if g := l.Stats().Generation; g != 1 {
		t.Fatalf("generation changed by empty batch: %d", g)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	_, err := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Set, "d", 0, 4}, {Delete, "a", 0, 0}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("got %v want ErrCapacity", err)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("capacity failure did not roll back")
	}
}

func TestTopAndSnapshotOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}}); err != nil {
		t.Fatal(err)
	}
	top, err := l.Top(3)
	if err != nil || len(top) != 3 {
		t.Fatal(top, err)
	}
	want := []string{"c", "a", "b"}
	for i, a := range top {
		if a.Name != want[i] {
			t.Fatalf("top[%d]=%s want %s", i, a.Name, want[i])
		}
	}
	snap := l.Snapshot()
	names := []string{"a", "b", "c", "d"}
	for i, a := range snap.Accounts {
		if a.Name != names[i] {
			t.Fatalf("snapshot[%d]=%s want %s", i, a.Name, names[i])
		}
	}
	snap.Accounts[0].Value = 999
	if l.Snapshot().Accounts[0].Value == 999 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestChangedOrderAndDelete(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{Ops: []Op{{Set, "x", 0, 1}, {Add, "x", 2, 0}, {Set, "y", 0, 4}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "x" || r.Changed[0].Value != 3 || r.Changed[0].Revision != 2 {
		t.Fatalf("changed: %+v", r.Changed)
	}
	r, err = l.Apply(Batch{Ops: []Op{{Delete, "x", 0, 0}}})
	if err != nil || len(r.Changed) != 0 {
		t.Fatalf("delete changed: %+v %v", r.Changed, err)
	}
}

func TestCloneClocksAndIsolation(t *testing.T) {
	l := led(t)
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 7}}}); err != nil {
		t.Fatal(err)
	}
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone lost logical clocks")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Set, "b", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	if l.Stats().Accounts != 1 || l.Stats().Generation != 1 {
		t.Fatal("clone mutation leaked into original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 25; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				_, _, _, _ = l.Preview(Batch{Ops: []Op{{Set, k, 0, 5}}})
				_, _ = l.Clone()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Add, k, 1, 0}}})
			}
		}()
	}
	w.Wait()
	s := l.Stats()
	if s.Accounts != 16 {
		t.Fatalf("accounts=%d", s.Accounts)
	}
	for _, a := range l.Snapshot().Accounts {
		if a.Value != 25 {
			t.Fatalf("%s=%d want 25", a.Name, a.Value)
		}
	}
}
