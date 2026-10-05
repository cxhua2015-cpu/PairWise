package auctionqueue

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
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongid", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "z-0_", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, e = q.Apply(Batch{Ops: []Op{{Kind(3), "a", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNegativeInput(t *testing.T) {
	q := queue(t)
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

func TestMonotonicTime(t *testing.T) {
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
	if s := q.Snapshot(); s.Now != 5 || len(s.Items) != 1 {
		t.Fatal(s)
	}
	// 失败批次不回滚时间之外的状态，也不推进时间。
	if _, e := q.Apply(Batch{Now: 7, Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	r, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	if r.Generation != 2 || r.Revision != 0 {
		t.Fatal(r)
	}
	s := q.Snapshot()
	if s.Generation != 2 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	// 失败批次不消耗 revision。
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s = q.Snapshot(); s.NextRevision != 3 || s.Generation != 2 {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); len(s.Items) != 0 || s.NextRevision != 1 || s.Generation != 0 {
		t.Fatal(s)
	}
	// 同批次内 cancel 后再 enqueue 不触发容量上限。
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if _, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestExistsAndNotFound(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "b", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestPopOrderAndPartial(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Now: 10, Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 3, 5}, {Enqueue, "c", 3, 2},
		{Enqueue, "d", 3, 2}, {Enqueue, "e", 2, 20},
	}})
	got, e := q.Pop(10, 3)
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if !reflect.DeepEqual(ids, []string{"c", "d", "b"}) {
		t.Fatal(ids)
	}
	// "e" 未就绪，只剩 "a"。
	got, _ = q.Pop(10, 10)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
	}
	if s := q.Snapshot(); len(s.Items) != 1 || s.Items[0].ID != "e" {
		t.Fatal(s)
	}
	if got, e = q.Pop(10, 1); e != nil || len(got) != 0 {
		t.Fatal(got, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
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
			now := int64(i + 1)
			_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Pop(now, 1)
			_ = q.Snapshot()
			_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Cancel, id, 0, 0}}})
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
	for _, it := range s.Items {
		if !validID(it.ID, 16) {
			t.Fatal(it.ID)
		}
	}
}
