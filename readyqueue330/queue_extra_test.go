package readyqueue330

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
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongid", "中文"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatal(id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c", "0-9_z"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatal(id, e)
		}
	}
}

func TestStructuralBeforeState(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	// Second op is structurally invalid; first would be ErrExists.
	// Structural validation of the whole batch must win.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Kind: 99, ID: "b"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonicAndRollback(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// Failed batch must not advance time or consume revisions.
	s := q.Snapshot()
	if s.Now != 5 || s.NextRevision != 2 || len(s.Items) != 1 {
		t.Fatal(s)
	}
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Pop with older now is a time error; equal now is fine.
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 1); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionRollbackOnCapacity(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 0, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.NextRevision != 2 || s.Generation != 1 || len(s.Items) != 1 {
		t.Fatal(s)
	}
	// Net-zero batch exceeding capacity mid-way is fine: check is final only.
	r, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "b", 0, 0}, {Enqueue, "c", 0, 0}, {Cancel, "c", 0, 0}}})
	if e != nil || r.Revision != 3 {
		t.Fatal(r, e)
	}
}

func TestGenerationSemantics(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if q.Snapshot().Generation != 0 {
		t.Fatal("empty batch changed generation")
	}
	r, _ = q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	_, _ = q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "x", 0, 0}, {Cancel, "zz", 0, 0}}})
	if q.Snapshot().Generation != 1 {
		t.Fatal("failed batch changed generation")
	}
}

func TestPopOrderAndLimit(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 3, 9},
		{Enqueue, "c", 3, 2},
		{Enqueue, "d", 3, 2},
	}})
	x, e := q.Pop(10, 2)
	if e != nil || len(x) != 2 || x[0].ID != "c" || x[1].ID != "d" {
		t.Fatal(e, x)
	}
	x, _ = q.Pop(10, 10)
	if len(x) != 2 || x[0].ID != "b" || x[1].ID != "a" {
		t.Fatal(x)
	}
	if len(q.Snapshot().Items) != 0 {
		t.Fatal("pop did not delete")
	}
	if _, e = q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestPopReadinessFilter(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "later", 9, 5}, {Enqueue, "now", 1, 0}}})
	x, e := q.Pop(4, 10)
	if e != nil || len(x) != 1 || x[0].ID != "now" {
		t.Fatal(e, x)
	}
	x, _ = q.Pop(5, 10)
	if len(x) != 1 || x[0].ID != "later" {
		t.Fatal(x)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "b"})
	if got := q.Snapshot(); len(got.Items) != 1 || got.Items[0].ID != "a" {
		t.Fatal(got)
	}
	p, _ := q.Pop(1, 1)
	p[0].ID = "mutated"
	if q.Snapshot().NextRevision != 2 {
		t.Fatal("pop result aliases internal state")
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
			for n := 0; n < 20; n++ {
				_, _ = q.Apply(Batch{Now: int64(n), Ops: []Op{{Enqueue, id, n, int64(n % 3)}}})
				_, _ = q.Pop(int64(n), 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(n), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
	}
	if len(s.Items) > 256 {
		t.Fatal("capacity exceeded")
	}
}

func TestResultFields(t *testing.T) {
	q := queue(t)
	r, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || s.Items[0].Revision != 1 || s.Items[1].Revision != 2 {
		t.Fatal(s)
	}
	if !reflect.DeepEqual([]string{"a", "b"}, []string{s.Items[0].ID, s.Items[1].ID}) {
		t.Fatal(s.Items)
	}
}
