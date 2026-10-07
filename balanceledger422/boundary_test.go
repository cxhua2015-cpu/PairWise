package balanceledger422

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {4, 8, -1}, {-1, 8, 10},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 9, Name: "a"}}},
		{Ops: []Op{{Kind: Add, Name: "a"}}}, // zero delta

		{Ops: []Op{{Kind: Add, Name: "a", Delta: 1, Value: 1}}},
		{Ops: []Op{{Kind: Set, Name: "a", Delta: 1}}},
		{Ops: []Op{{Kind: Delete, Name: "a", Delta: 1}}},
		{Ops: []Op{{Kind: Delete, Name: "a", Value: 1}}},
		{Ops: []Op{{Kind: Set, Name: ""}}},
		{Ops: []Op{{Kind: Set, Name: "Upper"}}},
		{Ops: []Op{{Kind: Set, Name: "has space"}}},
		{Ops: []Op{{Kind: Set, Name: "waytoolongname"}}},
		{Ops: []Op{{Kind: Set, Name: "é"}}},
	}
	for i, b := range bad {
		if err := l.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := l.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := l.ValidateBatch(Batch{}); err != nil {
		t.Fatal(err)
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "ok-nm_1", -3, 0}, {Set, "b", 0, 0}, {Delete, "c", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if s := l.Stats(); s.Generation != 0 || s.NextRevision != 1 || s.Accounts != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("positive overflow: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("minint set: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("negative overflow: %v", err)
	}
	small, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 5})
	for _, b := range []Batch{
		{Ops: []Op{{Set, "a", 0, 6}}},
		{Ops: []Op{{Set, "a", 0, -6}}},
		{Ops: []Op{{Add, "a", 6, 0}}},
	} {
		if _, err := small.Apply(b); !errors.Is(err, ErrValue) {
			t.Fatalf("abs limit: %v", err)
		}
	}
	if _, err := small.Apply(Batch{Ops: []Op{{Set, "a", 0, -5}}}); err != nil {
		t.Fatal(err)
	}
}

func TestCapacityRollbackAndRevisionGaps(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}}}); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	after := l.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed batch mutated state")
	}
	if after.NextRevision != 3 {
		t.Fatalf("failed batch consumed revisions: %d", after.NextRevision)
	}
	// Delete-then-create within one batch frees capacity (checked only at end).
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "c", 0, 3}}}); err != nil {
		t.Fatal(err)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1}}})
	top, err := l.Top(3)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{top[0].Name, top[1].Name, top[2].Name}
	if !reflect.DeepEqual(names, []string{"c", "a", "b"}) {
		t.Fatal(names)
	}
	top[0].Value = -999
	if l.Snapshot().Accounts[2].Name != "c" || l.Snapshot().Accounts[2].Value != 9 {
		t.Fatal("top aliases internal state")
	}
	if _, err := l.Top(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(len(all))
	}
	if empty, _ := l.Top(0); len(empty) != 0 {
		t.Fatal(empty)
	}
	snap := l.Snapshot()
	snap.Accounts[0].Value = 12345
	if l.Snapshot().Accounts[0].Value == 12345 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = l.Apply(Batch{Ops: []Op{}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatalf("empty batch bumped clocks: %+v", r)
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 7}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone lost logical clocks")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if l.Snapshot().Accounts[0].Value != 7 || l.Stats().NextRevision != 2 {
		t.Fatal("clone mutation leaked into original")
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
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			for j := 0; j < 20; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
				_ = l.Stats()
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
				_, _, _, _ = l.Preview(Batch{Ops: []Op{{Add, k, 1, 0}}})
			}
		}()
	}
	w.Wait()
	s := l.Stats()
	if s.Accounts != 32 || s.Generation != 32*20 || s.NextRevision != 1+32*20 {
		t.Fatalf("%+v", s)
	}
	var total int64
	for _, a := range l.Snapshot().Accounts {
		total += a.Value
	}
	if total != 32*20 {
		t.Fatal(total)
	}
}

func TestPreviewEmptyBatch(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 2}}})
	r, snap, st, err := l.Preview(Batch{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 1 || snap.Generation != 1 || st.Generation != 1 || st.NextRevision != 2 {
		t.Fatalf("%+v %+v %+v", r, snap, st)
	}
}
