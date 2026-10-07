package readyqueue400

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
	bad := []string{"", "A", "a b", "a.b", "a/b", "中文", "toolongid"}
	for _, id := range bad {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefgh"}
	for _, id := range good {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeState(t *testing.T) {
	q := queue(t)
	// Second op is structurally invalid; first op must not be applied.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind: 0, ID: "b"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
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
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Failed time checks must not change state.
	s := q.Snapshot()
	if s.Now != 5 || s.Generation != 1 || len(s.Items) != 1 {
		t.Fatal(s)
	}
}

func TestPopInvalidInput(t *testing.T) {
	q := queue(t)
	for _, args := range [][2]int64{{0, 0}, {0, -1}} {
		if _, e := q.Pop(args[0], int(args[1])); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(args, e)
		}
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	// Empty batch: generation unchanged.
	r3, e := q.Apply(Batch{})
	if e != nil || r3.Generation != 2 {
		t.Fatal(r3, e)
	}
	s := q.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
	// Revisions are visible on items and never reused.
	for _, it := range s.Items {
		if it.ID == "b" && it.Revision != 2 {
			t.Fatal(it)
		}
		if it.ID == "c" && it.Revision != 3 {
			t.Fatal(it)
		}
	}
}

func TestRollbackRestoresRevision(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	b := q.Snapshot()
	// Fails on duplicate; the enqueued "b" revision must be rolled back.
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("state changed after rollback")
	}
	// Next successful enqueue reuses the rolled-back revision.
	r, _ := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "c", 1, 0}}})
	if r.Revision != 2 {
		t.Fatal(r)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	b := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 2, 5},
		{Enqueue, "c", 2, 1},
		{Enqueue, "d", 2, 1},
		{Enqueue, "e", 9, 100},
	}})
	// "e" not ready; order among ready: c, d (prio 2, ready 1, id asc), a, b.
	x, e := q.Pop(10, 10)
	if e != nil {
		t.Fatal(e)
	}
	got := []string{}
	for _, it := range x {
		got = append(got, it.ID)
	}
	if !reflect.DeepEqual(got, []string{"c", "d", "b", "a"}) {
		t.Fatal(got)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
	// Limit respected.
	_, _ = q.Apply(Batch{Now: 10, Ops: []Op{{Enqueue, "f", 1, 0}, {Enqueue, "g", 1, 0}}})
	x, _ = q.Pop(10, 1)
	if len(x) != 1 {
		t.Fatal(x)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentApplyPop(t *testing.T) {
	q, _ := New(Options{MaxItems: 512, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-%04d", g, i)
				_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, id, i, 0}}})
			}
		}()
	}
	popped := make(chan int, 8)
	for p := 0; p < 4; p++ {
		w.Add(1)
		go func() {
			defer w.Done()
			n := 0
			for i := 0; i < 100; i++ {
				x, _ := q.Pop(0, 4)
				n += len(x)
			}
			popped <- n
		}()
	}
	w.Wait()
	close(popped)
	total := 0
	for n := range popped {
		total += n
	}
	if total+len(q.Snapshot().Items) != 400 {
		t.Fatal(total, len(q.Snapshot().Items))
	}
}

func TestConcurrentCancelSameID(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	var w sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}}); e == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	w.Wait()
	if ok != 1 {
		t.Fatal(ok)
	}
}
