package balanceledger377

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
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l := led(t)
	for _, b := range []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Kind: Add, Name: ""}}},
		{Ops: []Op{{Kind: Add, Name: "Upper"}}},
		{Ops: []Op{{Kind: Add, Name: "has space"}}},
		{Ops: []Op{{Kind: Add, Name: "dot.com"}}},
		{Ops: []Op{{Kind: Add, Name: "中文"}}},
		{Ops: []Op{{Kind: Add, Name: "012345678"}}}, // 9 bytes > MaxNameBytes 8
		{Ops: []Op{{Kind: Add, Name: "ok", Delta: 1}, {Kind: Delete, Name: "bad!"}}},
	} {
		if _, e := l.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal("generation moved on invalid input", g)
	}
}

func TestValidNameCharset(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a-z_09", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal("positive overflow", e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -2, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal("negative overflow", e)
	}
	s := l.Snapshot()
	if s.Accounts[0].Value != math.MaxInt64 || s.Accounts[1].Value != -math.MaxInt64 {
		t.Fatal("state corrupted after overflow", s)
	}

	m, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	for _, b := range []Batch{
		{Ops: []Op{{Set, "a", 0, 11}}},
		{Ops: []Op{{Set, "a", 0, -11}}},
		{Ops: []Op{{Add, "a", 11, 0}}},
		{Ops: []Op{{Set, "a", 0, 5}, {Add, "a", 6, 0}}},
	} {
		if _, e := m.Apply(b); !errors.Is(e, ErrValue) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	if _, e := m.Apply(Batch{Ops: []Op{{Set, "a", 0, 10}, {Add, "a", -20, 0}}}); e != nil {
		t.Fatal("boundary values should pass", e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal("empty batch", r, e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal("create-then-delete should not appear in Changed", r.Changed)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 || len(s.Accounts) != 1 || s.Accounts[0].Revision != 2 {
		t.Fatal(s)
	}
	// Failed batch must not consume revisions or generation.
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "c", 0, 1}, {Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s = l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 || len(s.Accounts) != 1 {
		t.Fatal("rollback leaked state", s)
	}
}

func TestDeletePreExistingInChanged(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Name != "a" || r.Changed[0].Value != 1 {
		t.Fatal(r, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestChangedIsolation(t *testing.T) {
	l := led(t)
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Add, "a", 1, 0}, {Set, "b", 0, 3}}})
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[0].Value != 2 || r.Changed[0].Revision != 2 {
		t.Fatal(r.Changed)
	}
	r.Changed[0].Value = 999
	top, _ := l.Top(1)
	if top[0].Value == 999 {
		t.Fatal("returned slice aliases internal state")
	}
	snap := l.Snapshot()
	snap.Accounts[0].Value = 999
	if l.Snapshot().Accounts[0].Value == 999 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, _ := l.Top(3)
	want := []string{"c", "a", "b"}
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatal("Top should clamp to account count")
	}
	if none, e := l.Top(0); e != nil || len(none) != 0 {
		t.Fatal("Top(0)", none, e)
	}
	if s := l.Snapshot(); s.Accounts[0].Name != "a" || s.Accounts[3].Name != "d" {
		t.Fatal("snapshot not name-sorted", s)
	}
}

func TestConcurrentStress(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				if _, e := l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}}); e != nil {
					t.Error(e)
					return
				}
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 16 || s.Generation != 16*50 {
		t.Fatal(s.Generation, len(s.Accounts))
	}
	var sum int64
	for _, a := range s.Accounts {
		sum += a.Value
	}
	if sum != 16*50 {
		t.Fatal("lost updates", sum)
	}
}
