package resourceledger117

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func opts() Options { return Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100} }

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	l, _ := New(opts())
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Add, "", 1, 0}}},
		{Ops: []Op{{Add, "A", 1, 0}}},
		{Ops: []Op{{Add, "a b", 1, 0}}},
		{Ops: []Op{{Add, "toolongname", 1, 0}}},
		{Ops: []Op{{Add, "é", 1, 0}}},
	}
	for _, b := range bad {
		if _, e := l.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", b, e)
		}
	}
	// 结构校验失败不得改变 generation。
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
	// 合法字符：小写、数字、连字符、下划线。
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a-1_b", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	l, _ := New(opts())
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if g := l.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestOverflow(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// MinInt64 的绝对值不可表示，超出任何合法上限。
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if got := l.Snapshot().Accounts[0].Value; got != math.MinInt64+1 {
		t.Fatal(got)
	}
}

func TestAbsLimit(t *testing.T) {
	l, _ := New(opts())
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 101}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -101}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 100, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -200, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	b := l.Snapshot()
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := l.Snapshot(); s.Generation != b.Generation || len(s.Accounts) != 1 || s.Accounts[0].Name != "a" {
		t.Fatal(s)
	}
}

func TestDeleteNotFoundAndRevisions(t *testing.T) {
	l, _ := New(opts())
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "x", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}}})
	if r.Revision != 2 || r.Generation != 1 || len(r.Changed) != 2 {
		t.Fatal(r)
	}
	s := l.Snapshot()
	if s.NextRevision != 3 || len(s.Accounts) != 1 || s.Accounts[0].Name != "b" {
		t.Fatal(s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(opts())
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3}, {Set, "d", 0, 9},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "d" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(all)
	}
	// 返回切片与内部状态隔离。
	all[0].Value = -999
	again, _ := l.Top(1)
	if again[0].Value != 9 {
		t.Fatal(again)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l, _ := New(opts())
	r, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	r.Changed[0].Value = 42
	s := l.Snapshot()
	s.Accounts[0].Value = 42
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("snapshot not isolated")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
			_, _ = l.Apply(Batch{Ops: []Op{{Delete, k, 0, 0}, {Set, k, 0, 50}}})
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 16 {
		t.Fatal(len(s.Accounts))
	}
	for _, a := range s.Accounts {
		if a.Value != 50 {
			t.Fatal(a)
		}
	}
}
