package balanceledger367

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 10}, {4, 0, 10}, {4, 8, 0}, {-1, 8, 10}, {4, -1, 10}, {4, 8, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, name := range []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, e)
		}
	}
	for _, name := range []string{"a", "z-0_", "12345678"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); e != nil {
			t.Fatalf("name %q: %v", name, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(99), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, -math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// 上限在算术前检测：MaxAbsValue 之外的新建 delta 直接拒绝。
	l2 := led(t)
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "a", 21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "a", -21, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	r, e = l.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
	if s := l.Snapshot(); s.Generation != 1 || s.NextRevision != 2 {
		t.Fatal(s)
	}
}

func TestRevisionSequence(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Add, "b", 1, 0}}})
	if e != nil || r.Revision != 3 {
		t.Fatal(r, e)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Changed[0].Value != 3 || r.Changed[0].Revision != 3 {
		t.Fatal(r.Changed)
	}
	s := l.Snapshot()
	if s.NextRevision != 4 || len(s.Accounts) != 1 || s.Accounts[0].Name != "b" {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
	// 先增后删，批次末容量满足则成功。
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Set, "b", 0, 2}, {Set, "c", 0, 3}, {Delete, "c", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, -3}, {Set, "d", 0, 9}}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	if top[0].Name != "d" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "c" {
		t.Fatal(all)
	}
	// 返回切片与内部状态隔离。
	top[0].Value = -99
	again, _ := l.Top(1)
	if again[0].Value != 9 {
		t.Fatal(again)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}, {Set, "a", 0, 1}}})
	s := l.Snapshot()
	if len(s.Accounts) != 2 || s.Accounts[0].Name != "a" || s.Accounts[1].Name != "b" {
		t.Fatal(s)
	}
	s.Accounts[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("snapshot not isolated")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1000})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Set, k, 0, int64(j)}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
			_, _ = l.Apply(Batch{Ops: []Op{{Delete, k, 0, 0}}})
		}()
	}
	w.Wait()
	if n := len(l.Snapshot().Accounts); n != 0 {
		t.Fatal(n)
	}
	if g := l.Snapshot().Generation; g != 16*50*2+16 {
		t.Fatal(g)
	}
}
