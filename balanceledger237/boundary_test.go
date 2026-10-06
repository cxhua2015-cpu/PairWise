package balanceledger237

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, 1, -5},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestValidateBatchStructural(t *testing.T) {
	l := led(t)
	cases := []struct {
		name string
		op   Op
		err  error
	}{
		{"unknown kind", Op{Kind: 0, Name: "a"}, ErrInvalidInput},
		{"kind out of range", Op{Kind: 99, Name: "a"}, ErrInvalidInput},
		{"empty name", Op{Kind: Set, Name: "", Value: 1}, ErrInvalidInput},
		{"uppercase name", Op{Kind: Set, Name: "Ab", Value: 1}, ErrInvalidInput},
		{"non-ascii name", Op{Kind: Set, Name: "é", Value: 1}, ErrInvalidInput},
		{"name too long", Op{Kind: Set, Name: "abcdefghi", Value: 1}, ErrInvalidInput},
		{"add zero delta", Op{Kind: Add, Name: "a"}, ErrInvalidInput},
		{"add extra value", Op{Kind: Add, Name: "a", Delta: 1, Value: 1}, ErrInvalidInput},
		{"set extra delta", Op{Kind: Set, Name: "a", Delta: 1, Value: 1}, ErrInvalidInput},
		{"delete extra fields", Op{Kind: Delete, Name: "a", Value: 1}, ErrInvalidInput},
		{"delta over abs limit", Op{Kind: Add, Name: "a", Delta: 21}, ErrValue},
		{"set over abs limit", Op{Kind: Set, Name: "a", Value: -21}, ErrValue},
		{"set min int64", Op{Kind: Set, Name: "a", Value: math.MinInt64}, ErrValue},
	}
	for _, c := range cases {
		if err := l.ValidateBatch(Batch{Ops: []Op{c.op}}); !errors.Is(err, c.err) {
			t.Errorf("%s: got %v want %v", c.name, err, c.err)
		}
		if _, err := l.Apply(Batch{Ops: []Op{c.op}}); !errors.Is(err, c.err) {
			t.Errorf("apply %s: got %v want %v", c.name, err, c.err)
		}
	}
	if s := l.Stats(); s.Accounts != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestOverflowPreCheck(t *testing.T) {
	l, err := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("positive overflow: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", -2, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("negative overflow: %v", err)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MinInt64+1 {
		t.Fatalf("state mutated: %d", got)
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t)
	if _, err := l.Apply(Batch{Ops: []Op{
		{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3},
		{Set, "d", 0, 4}, {Set, "e", 0, 5},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("want ErrCapacity, got %v", err)
	}
	s := l.Stats()
	if s.Accounts != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Add, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	s := l.Stats()
	if s.Generation != 2 || s.NextRevision != 3 {
		t.Fatalf("clocks: %+v", s)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if s2 := l.Stats(); s2 != s {
		t.Fatalf("failed batch advanced clocks: %+v -> %+v", s, s2)
	}
}

func TestTopOrderAndIsolation(t *testing.T) {
	l := led(t)
	if _, err := l.Top(0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("n=0 must fail")
	}
	if _, err := l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3},
	}}); err != nil {
		t.Fatal(err)
	}
	top, err := l.Top(2)
	if err != nil || len(top) != 2 || top[0].Name != "a" || top[1].Name != "b" {
		t.Fatalf("top: %+v %v", top, err)
	}
	top[0].Value = -100
	if l.Snapshot().Accounts[0].Value != 5 {
		t.Fatal("returned slice aliases internal state")
	}
	snap := l.Snapshot()
	names := []string{snap.Accounts[0].Name, snap.Accounts[1].Name, snap.Accounts[2].Name}
	if names[0] != "a" || names[1] != "b" || names[2] != "c" {
		t.Fatalf("snapshot not name-sorted: %v", names)
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 3}}}); err != nil {
		t.Fatal(err)
	}
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 0 {
		t.Fatal("clone and original are not independent")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 20; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				if j%7 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := l.Stats()
	if s.Accounts != 32 || s.NextRevision != 32*20+1 {
		t.Fatalf("lost updates: %+v", s)
	}
}
