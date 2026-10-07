package readyqueue310

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
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongid", "a+b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefgh"}
	for _, id := range good {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestStructuralBeforeState(t *testing.T) {
	q := queue(t)
	// 未知 kind 与额外非法输入必须先于状态检查失败。
	_, e := q.Apply(Batch{Now: -1, Ops: []Op{{Kind: 99, ID: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "ok", 1, -1}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
}

func TestEmptyBatchNoChange(t *testing.T) {
	q := queue(t)
	b := q.Snapshot()
	r, e := q.Apply(Batch{Now: 3})
	if e != nil {
		t.Fatal(e)
	}
	if r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("empty batch mutated state")
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	if r2.Generation != 2 || r2.Revision != 2 {
		t.Fatal(r2)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || s.Items[0].Revision != 2 {
		t.Fatal(s)
	}
}

func TestFailedBatchRollsBackRevision(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.NextRevision != 2 || len(s.Items) != 1 {
		t.Fatal(s)
	}
	// 容量溢出同样回滚 revision。
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}, {Enqueue, "e", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.NextRevision != 2 || len(s.Items) != 1 {
		t.Fatal(s)
	}
}

func TestPopOrderAndAtomicDelete(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 2, 5},
		{Enqueue, "c", 2, 3},
		{Enqueue, "d", 2, 3},
		{Enqueue, "e", 9, 100},
	}})
	got, e := q.Pop(10, 3)
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if !reflect.DeepEqual(ids, []string{"c", "d", "b"}) {
		t.Fatal(ids)
	}
	if len(q.Snapshot().Items) != 2 {
		t.Fatal("pop did not delete atomically")
	}
	// "e" 尚未就绪，只剩 "a" 可弹出。
	got, _ = q.Pop(10, 5)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	s.Items = append(s.Items, Item{ID: "zz"})
	if got := q.Snapshot(); len(got.Items) != 1 || got.Items[0].ID != "a" {
		t.Fatal("snapshot shares state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("task-%d", i)
			for j := 0; j < 50; j++ {
				_, _ = q.Apply(Batch{Now: int64(j), Ops: []Op{{Enqueue, id, j, 0}}})
				_, _ = q.Pop(int64(j), 1)
				_, _ = q.Apply(Batch{Now: int64(j), Ops: []Op{{Cancel, id, 0, 0}}})
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	if n := len(q.Snapshot().Items); n > 256 {
		t.Fatal(n)
	}
}
