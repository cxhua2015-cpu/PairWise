package balanceledger237

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestOverflowGuard(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal("positive overflow:", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal("negative overflow:", err)
	}
	// MinInt64 itself can never satisfy the absolute-value bound.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "c", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatal("min int64:", err)
	}
	if got := l.Stats().Accounts; got != 2 {
		t.Fatal("state changed after failed batches:", got)
	}
}

func TestAbsLimitAndRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 10})
	before := l.Snapshot()
	_, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 5}, {Add, "a", 6, 0}}})
	if !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if got := l.Snapshot(); got.Generation != before.Generation || len(got.Accounts) != 0 {
		t.Fatal("failed batch leaked state:", got)
	}
}

func TestValidationSharedSemantics(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 4, MaxAbsValue: 10})
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Add, "", 1, 0}}},
		{Ops: []Op{{Add, "Upper", 1, 0}}},
		{Ops: []Op{{Add, "a b", 1, 0}}},
		{Ops: []Op{{Add, "toolong", 1, 0}}},
		{Ops: []Op{{Add, "a", 0, 0}}},
		{Ops: []Op{{Add, "a", 1, 1}}},
		{Ops: []Op{{Set, "a", 1, 0}}},
		{Ops: []Op{{Delete, "a", 1, 0}}},
	}
	for i, b := range bad {
		if err := l.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d validate: %v", i, err)
		}
		if _, err := l.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d apply: %v", i, err)
		}
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "a-_0", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if err := l.ValidateBatch(Batch{}); err != nil {
		t.Fatal("empty batch must be valid:", err)
	}
}

func TestEmptyBatchAndClocks(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = l.Apply(Batch{})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal("empty batch must not advance generation:", r)
	}
}

func TestDeleteMissingAndCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "ghost", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	// Net-zero final capacity is fine mid-batch overflow must roll back.
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := l.Stats(); got.Accounts != 1 || got.Generation != 1 {
		t.Fatalf("capacity failure leaked: %+v", got)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 {
		t.Fatal(top, err)
	}
	want := []string{"c", "a", "b"}
	for i, name := range want {
		if top[i].Name != name {
			t.Fatal(top)
		}
	}
	top[0].Value = -999
	if again, _ := l.Top(1); again[0].Value != 9 {
		t.Fatal("Top result aliases internal state")
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatal(all)
	}
	snap := l.Snapshot()
	snap.Accounts[0].Value = 0
	if l.Snapshot().Accounts[0].Value == 0 {
		t.Fatal("Snapshot aliases internal state")
	}
}

func TestCloneOwnershipAndClocks(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 7}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got.Generation != 1 || got.NextRevision != 2 {
		t.Fatalf("clone lost logical clocks: %+v", got)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	r2, _ := c.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	if r1.Revision != r2.Revision || r1.Generation != r2.Generation {
		t.Fatal("clocks diverged:", r1, r2)
	}
	if l.Snapshot().Accounts[0].Value != 8 || c.Snapshot().Accounts[0].Value != 8 {
		t.Fatal("clone shares state with original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			name := string(rune('a'+i/8)) + string(rune('0'+i%8))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_ = l.Stats()
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Add, name, 1, 0}}})
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	st := l.Stats()
	if st.Accounts != 16 || st.Generation != 800 {
		t.Fatalf("%+v", st)
	}
	if st.NextRevision != 801 {
		t.Fatalf("revisions not consecutive: %+v", st)
	}
}
