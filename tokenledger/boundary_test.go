package tokenledger

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{MaxAccounts: 0, MaxNameBytes: 8, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 0, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 0},
		{MaxAccounts: -1, MaxNameBytes: 8, MaxAbsValue: 1},
		{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"}
	for _, n := range bad {
		if _, e := l.Apply(Batch{Ops: []Op{{Add, n, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
	good := []string{"a", "z0-_", "01234567"}
	for _, n := range good {
		if _, e := l.Apply(Batch{Ops: []Op{{Set, n, 0, 1}}}); e != nil {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	for _, k := range []Kind{0, 4, 255} {
		if _, e := l.Apply(Batch{Ops: []Op{{Kind: k, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: got %v", k, e)
		}
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
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// MinInt64 绝对值不可表示，必须拒绝。
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "c", math.MinInt64, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestAbsLimit(t *testing.T) {
	l := led(t) // MaxAbsValue = 20
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -21}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 20}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -41, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 100})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
	// 批次中间容量临时合法但末尾超限，必须整体回滚。
	_, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}, {Delete, "a", 0, 0}, {Set, "c", 0, 3}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Name != "a" || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r0, e := l.Apply(Batch{}) // 空批次：generation 不变
	if e != nil || r0.Generation != 0 {
		t.Fatal(r0, e)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Set, "b", 0, 2}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r2, _ := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if r2.Generation != 2 || r2.Revision != 2 { // Delete 不分配 revision
		t.Fatal(r2)
	}
	s := l.Snapshot()
	if s.Generation != 2 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestChangedContents(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{Ops: []Op{{Add, "b", 1, 0}, {Add, "a", 1, 0}, {Add, "b", 1, 0}, {Set, "c", 0, 1}, {Delete, "c", 0, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	// Changed 按名称排序、每账户一条最终状态，已删除账户不出现。
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[1].Name != "b" {
		t.Fatal(r.Changed)
	}
	if r.Changed[1].Value != 2 || r.Changed[1].Revision != 3 {
		t.Fatal(r.Changed[1])
	}
}

func TestTopOrder(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	want := []string{"c", "a", "b"} // 值降序，同值名称升序
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 {
		t.Fatal(all)
	}
	empty, _ := l.Top(0)
	if len(empty) != 0 {
		t.Fatal(empty)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	top, _ := l.Top(1)
	snap := l.Snapshot()
	top[0].Value = 99
	snap.Accounts[0].Value = 99
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("returned slices alias internal state")
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
			name := fmt.Sprintf("acct-%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Set, name, 0, int64(j)}}})
				_, _ = l.Top(10)
				_ = l.Snapshot()
			}
			_, _ = l.Apply(Batch{Ops: []Op{{Delete, name, 0, 0}}})
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 0 {
		t.Fatal(len(s.Accounts))
	}
	if s.Generation != 32*50*2+32 {
		t.Fatal(s.Generation)
	}
}
