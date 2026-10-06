package balanceledger292

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {-1, 8, 10}, {4, 8, -5},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "汉字", "toolongname", "a.b"}
	for _, n := range bad {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	good := []string{"a", "z-0_9", "12345678"}
	for _, n := range good {
		if err := l.ValidateBatch(Batch{Ops: []Op{{Set, n, 0, 1}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 1, 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unknown kind accepted")
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "a", 0, 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("zero delta accepted")
	}
}

func TestOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("positive overflow: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("MinInt64 set: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("negative overflow: %v", err)
	}
	// Failed batches must not consume revisions.
	s := l.Snapshot()
	if s.NextRevision != 3 || s.Generation != 2 {
		t.Fatalf("clocks advanced on failure: %+v", s)
	}
}

func TestAbsLimitAndGeneration(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}, {Set, "b", 0, -20}}})
	if err != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(err, r)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 21}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("abs limit: %v", err)
	}
	// Empty batch: success, no generation bump.
	r, err = l.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatal(err, r)
	}
	if l.Snapshot().Generation != 1 {
		t.Fatal("empty batch bumped generation")
	}
}

func TestCapacityRollback(t *testing.T) {
	l := led(t)
	if _, err := l.Apply(Batch{Ops: []Op{
		{Set, "a", 0, 1}, {Set, "b", 0, 1}, {Set, "c", 0, 1}, {Set, "d", 0, 1}, {Set, "e", 0, 1},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatalf("capacity failure leaked accounts: %d", n)
	}
}

func TestTopOrderAndIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3}}})
	top, err := l.Top(2)
	if err != nil || len(top) != 2 || top[0].Name != "a" || top[1].Name != "b" {
		t.Fatal(err, top)
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative n: %v", err)
	}
	top[0].Value = -99
	if l.Snapshot().Accounts[0].Value != 5 {
		t.Fatal("Top result aliases internal state")
	}
	snap := l.Snapshot()
	names := []string{snap.Accounts[0].Name, snap.Accounts[1].Name, snap.Accounts[2].Name}
	if names[0] != "a" || names[1] != "b" || names[2] != "c" {
		t.Fatal("snapshot not name-sorted", names)
	}
	snap.Accounts[0].Value = -99
	if l.Snapshot().Accounts[0].Value != 5 {
		t.Fatal("Snapshot aliases internal state")
	}
}

func TestDeleteRecreate(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 3}}})
	r, err := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Add, "a", 7, 0}}})
	if err != nil || len(r.Changed) != 1 || r.Changed[0].Value != 7 {
		t.Fatal(err, r)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Delete, "a", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete: %v", err)
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone clocks differ")
	}
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}}})
	if len(c.Snapshot().Accounts) != 1 {
		t.Fatal("original mutation leaked into clone")
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
				_ = l.Stats()
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
				if j%7 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	snap := l.Snapshot()
	if st.Accounts != len(snap.Accounts) || st.Generation != snap.Generation || st.NextRevision != snap.NextRevision {
		t.Fatalf("inconsistent final state: %+v vs %+v", st, snap)
	}
}
