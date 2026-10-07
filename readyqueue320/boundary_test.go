package readyqueue320

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongid", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	good := []string{"a", "z0-_", "12345678"}
	for _, id := range good {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	q := queue(t)
	// 即使第一个 op 会触发状态错误，结构错误也必须优先报告。
	_, e := q.Apply(Batch{Ops: []Op{{Cancel, "missing", 0, 0}, {Kind: 42, ID: "x"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// 相同时间允许。
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r1, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1, e)
	}
	// 空批次不改变 generation。
	r2, e := q.Apply(Batch{Now: 1})
	if e != nil || r2.Generation != 1 || r2.Revision != 0 {
		t.Fatal(r2, e)
	}
	r3, e := q.Apply(Batch{Now: 1, Ops: []Op{{Cancel, "a", 0, 0}}})
	if e != nil || r3.Generation != 2 || r3.Revision != 0 {
		t.Fatal(r3, e)
	}
	s := q.Snapshot()
	if s.Generation != 2 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	// revision 不复用：重新入队同 ID 得到新 revision。
	r4, _ := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r4.Revision != 3 {
		t.Fatal(r4)
	}
}

func TestExistsNotFoundCapacity(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// 末尾容量检查：先超后减允许。
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "a", 0, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// 末尾仍超容量则失败且整体回滚。
	b := q.Snapshot()
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "d", 1, 0}, {Enqueue, "e", 1, 0}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal(e)
	}
}

func TestPopOrderAndAtomicRemoval(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 3, 5},
		{Enqueue, "c", 3, 2},
		{Enqueue, "d", 3, 2},
		{Enqueue, "e", 2, 1},
	}})
	// now=2 时只有 a/c/d/e 就绪；b 未就绪。
	got, e := q.Pop(2, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "d", "e", "a"}
	var ids []string
	for _, it := range got {
		ids = append(ids, it.ID)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatal(ids)
	}
	// 已弹出的项不会再次出现。
	got, _ = q.Pop(2, 10)
	if len(got) != 0 {
		t.Fatal(got)
	}
	got, _ = q.Pop(5, 1)
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatal(got)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mut"
	s.Items = append(s.Items, Item{ID: "zz"})
	p, _ := q.Pop(0, 1)
	if len(p) != 1 || p[0].ID != "a" {
		t.Fatal(p)
	}
	p[0].ID = "mut"
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%02d-%04d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate", it.ID)
		}
		seen[it.ID] = true
	}
}
