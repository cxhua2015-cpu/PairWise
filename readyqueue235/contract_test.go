package readyqueue235

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func queue(t *testing.T) *Queue {
	t.Helper()
	q, e := New(Options{MaxItems: 4, MaxIDBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	return q
}
func TestOrder(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "b", 2, 2}, {Enqueue, "a", 2, 2}, {Enqueue, "c", 3, 3}}})
	if e != nil {
		t.Fatal(e)
	}
	x, e := q.Pop(3, 2)
	if e != nil || x[0].ID != "c" || x[1].ID != "a" {
		t.Fatal(e, x)
	}
}
func TestRollback(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}})
	b := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Cancel, "z", 0, 0}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal(e)
	}
}
func TestFinalCapacityTime(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}})
	_, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "b", 1, 2}}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.Pop(-1, 1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	q, _ := New(Options{MaxItems: 64, MaxIDBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, k, i, 0}}})
			_ = q.Snapshot()
		}()
	}
	w.Wait()
	if len(q.Snapshot().Items) != 20 {
		t.Fatal(len(q.Snapshot().Items))
	}
}
