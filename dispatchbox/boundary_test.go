package dispatchbox

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongiddd"}
	for _, id := range bad {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c", "0", "z9_-"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndFullValidationFirst(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind(0), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// 结构校验先于状态读取：重复 ID + 未知 kind 应报 ErrInvalidInput 而非 ErrExists。
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Kind(9), "b", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
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
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
	// 失败批次不回滚已提交的时间，但也不前进。
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	if s := q.Snapshot(); s.Generation != 0 || s.Now != 3 {
		t.Fatal(s)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || s.Items[0].Revision != 1 || s.Items[1].Revision != 2 {
		t.Fatal(s)
	}
	// 失败批次不消耗 revision。
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if q.Snapshot().NextRevision != 3 {
		t.Fatal(q.Snapshot().NextRevision)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}}})
	b := q.Snapshot()
	// 末尾容量检查：净增超容量整体回滚。
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 0, 0}, {Cancel, "a", 0, 0}, {Enqueue, "d", 0, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Generation != b.Generation || s.NextRevision != b.NextRevision || len(s.Items) != 2 {
		t.Fatal(s)
	}
}

func TestPopSemantics(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 9, 10},
		{Enqueue, "c", 9, 2},
	}})
	// now=5：只有 a、c ready；b ReadyAt=10 不参与。
	x, e := q.Pop(5, 10)
	if e != nil || len(x) != 2 || x[0].ID != "c" || x[1].ID != "a" {
		t.Fatal(x, e)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal(q.Snapshot())
	}
	if _, e = q.Pop(10, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// 空 ready 集合返回空而非错误。
	x, e = q.Pop(9, 1)
	if e != nil || len(x) != 0 {
		t.Fatal(x, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	s.Items = append(s.Items, Item{ID: "zz"})
	if got := q.Snapshot(); len(got.Items) != 1 || got.Items[0].ID != "a" {
		t.Fatal(got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("task-%d", i)
			for now := int64(0); now < 50; now++ {
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Enqueue, id, i, now}}})
				_, _ = q.Pop(now, 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
}
