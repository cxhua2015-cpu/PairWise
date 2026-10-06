package readyqueue205

import (
	"errors"
	"fmt"
	"reflect"
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
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongid", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "0", "-", "_", "a-b_c9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	q := queue(t)
	for _, k := range []Kind{0, 3, 255} {
		if _, e := q.Apply(Batch{Ops: []Op{{k, "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(k, e)
		}
	}
}

func TestBatchValidatedBeforeState(t *testing.T) {
	q := queue(t)
	// 第二个 op 结构非法：即使第一个 op 会触发 ErrExists，也必须先报 ErrInvalidInput。
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Kind(0), "b", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNegativeTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(-1, 1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, -1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 1); e != nil { // 相等允许
		t.Fatal(e)
	}
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
}

func TestFailedApplyRollsBackTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	// 失败批次不得推进时间。
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := q.Snapshot().Now; got != 3 {
		t.Fatal(got)
	}
	// 容量失败同样回滚时间与 revision。
	b := q.Snapshot()
	_, e := q.Apply(Batch{Now: 9, Ops: []Op{
		{Enqueue, "b", 0, 0}, {Enqueue, "c", 0, 0},
		{Enqueue, "d", 0, 0}, {Enqueue, "e", 0, 0}, {Enqueue, "f", 0, 0},
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("capacity failure must roll back")
	}
}

func TestDuplicateAndCancelSemantics(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Cancel, "a", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// 上一个批次已回滚，a 仍存在；先成功取消。
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	// 同批次取消后可重新入队。
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Cancel, "a", 0, 0}, {Enqueue, "a", 3, 0}}}); e != nil {
		t.Fatal(e)
	}
	if got := q.Snapshot().Items; len(got) != 1 || got[0].Priority != 3 {
		t.Fatal(got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 0})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	r, _ = q.Apply(Batch{Now: 0, Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = q.Apply(Batch{Now: 0, Ops: []Op{{Cancel, "a", 0, 0}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
	s := q.Snapshot()
	if s.Generation != 2 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	// 失败批次不增加 generation。
	if _, e = q.Apply(Batch{Now: 0, Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if q.Snapshot().Generation != 2 {
		t.Fatal("generation changed on failure")
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Now: 0, Ops: []Op{
		{Enqueue, "p1a", 1, 0}, {Enqueue, "p2a", 2, 5}, {Enqueue, "p2b", 2, 3},
		{Enqueue, "p2c", 2, 3}, {Enqueue, "future", 9, 100},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(50, 10)
	if e != nil {
		t.Fatal(e)
	}
	var ids []string
	for _, it := range got {
		ids = append(ids, it.ID)
	}
	want := []string{"p2b", "p2c", "p2a", "p1a"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatal(ids, want)
	}
	// 原子删除：剩余只有未就绪任务。
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
	got, e = q.Pop(100, 10)
	if e != nil || len(got) != 1 || got[0].ID != "future" {
		t.Fatal(got, e)
	}
}

func TestPopLimit(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 0}}})
	got, e := q.Pop(0, 1)
	if e != nil || len(got) != 1 || got[0].ID != "b" {
		t.Fatal(got, e)
	}
	got, e = q.Pop(0, 0)
	if e != nil || len(got) != 0 {
		t.Fatal(got, e)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "zz"})
	if got := q.Snapshot().Items; len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
	}
	p, _ := q.Pop(0, 1)
	p[0].ID = "mutated"
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
				id := fmt.Sprintf("g%d-i%d", g, i)
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
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
	}
}
