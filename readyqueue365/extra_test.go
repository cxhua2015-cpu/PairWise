package readyqueue365

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongid", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "ok_id-1", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndNegativeNow(t *testing.T) {
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
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 1}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal(q.Snapshot().Now)
	}
}

func TestDuplicateAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 2}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// 末尾容量检查：净增超过上限整体回滚，revision 也回滚。
	s := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 1}, {Enqueue, "c", 1, 1}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := q.Snapshot(); got.Generation != s.Generation || got.NextRevision != s.NextRevision || len(got.Items) != 1 {
		t.Fatal(got)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}, {Enqueue, "b", 1, 1}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	if r2.Generation != 2 || r2.Revision != 2 {
		t.Fatal(r2)
	}
	r3, _ := q.Apply(Batch{})
	if r3.Generation != 2 {
		t.Fatal(r3)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || s.Generation != 2 {
		t.Fatal(s)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if q.Snapshot().Generation != 2 {
		t.Fatal("failed batch bumped generation")
	}
}

func TestPopPartialAndSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 10}, // 未就绪
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
	}})
	x, e := q.Pop(5, 10)
	if e != nil || len(x) != 2 || x[0].ID != "c" || x[1].ID != "b" {
		t.Fatal(e, x)
	}
	s := q.Snapshot()
	if len(s.Items) != 1 || s.Items[0].ID != "a" {
		t.Fatal(s)
	}
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
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
			id := fmt.Sprintf("id-%d", i)
			for j := 0; j < 50; j++ {
				_, _ = q.Apply(Batch{Now: int64(j), Ops: []Op{{Enqueue, id, j, int64(j % 3)}}})
				_, _ = q.Pop(int64(j), 1)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(j), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
}
