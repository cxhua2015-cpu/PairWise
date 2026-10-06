package balanceledger292

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	bad := []Op{
		{Kind: 0, Name: "a"},
		{Kind: 99, Name: "a"},
		{Add, "a", 0, 0},    // zero delta
		{Add, "a", 1, 1},    // extra value field
		{Set, "a", 1, 1},    // extra delta field
		{Delete, "a", 1, 0}, // extra delta
		{Delete, "a", 0, 1}, // extra value
		{Add, "", 1, 0},
		{Add, "A", 1, 0},
		{Add, "a b", 1, 0},
		{Add, "toolongname", 1, 0},
		{Add, "中文", 1, 0},
	}
	for _, op := range bad {
		if err := l.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	good := []Op{
		{Add, "a", 1, 0}, {Add, "a", -1, 0}, {Set, "b-2_c", 0, -5}, {Delete, "a", 0, 0},
	}
	for _, op := range good {
		if err := l.ValidateBatch(Batch{Ops: []Op{op}}); err != nil {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	small, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 5})
	if _, err := small.Apply(Batch{Ops: []Op{{Set, "a", 0, 6}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := small.Apply(Batch{Ops: []Op{{Set, "a", 0, -6}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if got := small.Snapshot().Generation; got != 0 {
		t.Fatal("failed batches must not change generation")
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	r, err = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
	r, err = l.Apply(Batch{})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
	if s := l.Stats(); s.Generation != 1 || s.NextRevision != 2 || s.Accounts != 1 {
		t.Fatal(s)
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
	if _, err := l.Top(0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	top[0].Value = -999
	if l.Snapshot().Accounts[2].Name != "c" || l.Snapshot().Accounts[2].Value != 9 {
		t.Fatal("returned slices must be isolated")
	}
	snap := l.Snapshot()
	names := []string{"a", "b", "c", "d"}
	for i, a := range snap.Accounts {
		if a.Name != names[i] {
			t.Fatal(snap.Accounts)
		}
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 3}}})
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != l.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "z", 0, 1}}})
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 1 {
		t.Fatal(l.Stats(), c.Stats())
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Delete, "z", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, name, 0, 1}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = l.Stats()
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	wg.Wait()
	s := l.Stats()
	if s.Accounts != 32 || s.NextRevision != 32*50+1 {
		t.Fatal(s)
	}
}
