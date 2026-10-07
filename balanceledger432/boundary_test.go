package balanceledger432

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
			t.Fatalf("opts %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	cases := []struct {
		name string
		op   Op
	}{
		{"unknown kind", Op{Kind: 9, Name: "a"}},
		{"zero kind", Op{Kind: 0, Name: "a"}},
		{"empty name", Op{Kind: Set, Name: "", Value: 1}},
		{"uppercase name", Op{Kind: Set, Name: "Ab", Value: 1}},
		{"space in name", Op{Kind: Set, Name: "a b", Value: 1}},
		{"name too long", Op{Kind: Set, Name: "abcdefghi", Value: 1}},
		{"add zero delta", Op{Kind: Add, Name: "a"}},
		{"add extra value", Op{Kind: Add, Name: "a", Delta: 1, Value: 1}},
		{"add delta over limit", Op{Kind: Add, Name: "a", Delta: 21}},
		{"set extra delta", Op{Kind: Set, Name: "a", Delta: 1, Value: 1}},
		{"set over limit", Op{Kind: Set, Name: "a", Value: 21}},
		{"set under limit", Op{Kind: Set, Name: "a", Value: -21}},
		{"delete extra fields", Op{Kind: Delete, Name: "a", Delta: 1}},
	}
	for _, c := range cases {
		b := Batch{Ops: []Op{c.op}}
		if err := l.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: ValidateBatch=%v", c.name, err)
		}
		if _, err := l.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: Apply=%v", c.name, err)
		}
	}
	if err := l.ValidateBatch(Batch{Ops: []Op{{Set, "a-b_1", 0, 20}, {Add, "a-b_1", -20, 0}, {Delete, "a-b_1", 0, 0}}}); err != nil {
		t.Fatal(err)
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
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", -2, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatalf("negative overflow: %v", err)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MinInt64+1 {
		t.Fatalf("state mutated by failed batch: %d", got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	l := led(t)
	before := l.Snapshot()
	r, err := l.Apply(Batch{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != before.Generation || !reflect.DeepEqual(l.Snapshot(), before) {
		t.Fatalf("empty batch changed state: %+v", r)
	}
}

func TestRevisionSequenceAndGeneration(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Add, "a", 1, 0}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 1 || r.Revision != 3 {
		t.Fatalf("gen/rev: %+v", r)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[1].Name != "b" {
		t.Fatalf("changed: %+v", r.Changed)
	}
	s := l.Snapshot()
	if s.NextRevision != 4 || s.Generation != 1 || len(s.Accounts) != 1 || s.Accounts[0].Revision != 3 {
		t.Fatalf("snapshot: %+v", s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1}}})
	top, err := l.Top(3)
	if err != nil || len(top) != 3 {
		t.Fatal(top, err)
	}
	want := []string{"c", "a", "b"}
	for i, name := range want {
		if top[i].Name != name {
			t.Fatalf("top[%d]=%s want %s", i, top[i].Name, name)
		}
	}
	if top, _ = l.Top(0); len(top) != 0 {
		t.Fatal(top)
	}
	if top, _ = l.Top(100); len(top) != 4 {
		t.Fatal(top)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("snapshot aliases internal state")
	}
	top, _ := l.Top(1)
	top[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("top aliases internal state")
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
				_, _, _, _ = l.Preview(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(4)
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
	if st.Accounts != 32 || st.Generation != uint64(32*50) || st.NextRevision != uint64(32*50)+1 {
		t.Fatalf("stats: %+v", st)
	}
}
