package eventqueue

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongiddd", "é"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c", "0-9_z"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKindAndTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, -1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	r2, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if r1.Generation != 1 || r2.Generation != 2 || r2.Revision != r1.Revision+2 {
		t.Fatal(r1, r2)
	}
	s := q.Snapshot()
	if s.Generation != 2 || s.NextRevision != r2.Revision+1 {
		t.Fatal(s)
	}
	// 空批次不增加 generation，失败批次也不增加。
	r3, _ := q.Apply(Batch{})
	if r3.Generation != 2 {
		t.Fatal(r3)
	}
	_, _ = q.Apply(Batch{Ops: []Op{{Cancel, "missing", 0, 0}}})
	if q.Snapshot().Generation != 2 {
		t.Fatal("failed batch bumped generation")
	}
	// Cancel 不消耗 revision。
	r4, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	if r4.Generation != 3 || q.Snapshot().NextRevision != s.NextRevision {
		t.Fatal(r4, q.Snapshot())
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	before := q.Snapshot()
	_, e := q.Apply(Batch{Now: 9, Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := q.Snapshot()
	if after.Now != before.Now || after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Items) != 2 {
		t.Fatalf("no rollback: %+v -> %+v", before, after)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// 同批次内 enqueue 后 cancel 合法。
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "x", 1, 0}, {Cancel, "x", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "x", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "low", 1, 0},
		{Enqueue, "future", 9, 100},
		{Enqueue, "b", 5, 2},
		{Enqueue, "a", 5, 2},
		{Enqueue, "early", 5, 1},
	}})
	got, e := q.Pop(50, 10)
	if e != nil || len(got) != 4 {
		t.Fatal(e, got)
	}
	want := []string{"early", "a", "b", "low"}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
	// 已弹出的 ID 可重新入队。
	if _, e = q.Apply(Batch{Now: 50, Ops: []Op{{Enqueue, "low", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0] = Item{ID: "hacked"}
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
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
				id := fmt.Sprintf("g%d-%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	if n := len(q.Snapshot().Items); n > 256 {
		t.Fatal(n)
	}
}
