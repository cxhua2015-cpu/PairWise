package prioritybox

import (
	"errors"
	"fmt"
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
	bad := []string{"", "A", "a b", "a/b", "é", "toolongiddd"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKindAndBatchAtomicity(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation of the whole batch happens before state reads:
	// the second op is invalid, so the first must not be enqueued.
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Kind: 0, ID: "b"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 0 {
		t.Fatal("batch was not atomic")
	}
}

func TestTimeMonotonic(t *testing.T) {
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
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal("time moved on failure")
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	s := q.Snapshot()
	if s.Generation != 0 || s.Now != 0 {
		t.Fatal(s)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 0, 0}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	s := q.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
	// Failed batch must not consume revisions.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r3, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "d", 0, 0}}})
	if r3.Revision != 4 {
		t.Fatal(r3)
	}
}

func TestCapacityOnlyAtEnd(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	// Net size stays within capacity even though the batch adds 3 and removes 2.
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}, {Enqueue, "c", 0, 0},
		{Cancel, "a", 0, 0}, {Cancel, "b", 0, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Exceeding final capacity rolls everything back.
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "x", 0, 0}, {Enqueue, "y", 0, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal(q.Snapshot())
	}
}

func TestPopOrderAndNotReady(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "late", 9, 100},
		{Enqueue, "p1a", 1, 0},
		{Enqueue, "p2", 2, 5},
		{Enqueue, "p1b", 1, 0},
	}})
	got, e := q.Pop(10, 10)
	if e != nil || len(got) != 3 {
		t.Fatal(e, got)
	}
	want := []string{"p2", "p1a", "p1b"}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatal(got)
		}
	}
	if got[0].Revision == 0 {
		t.Fatal("missing revision")
	}
	// "late" remains; not ready yet.
	if len(q.Snapshot().Items) != 1 {
		t.Fatal(q.Snapshot())
	}
	if x, _ := q.Pop(99, 1); len(x) != 0 {
		t.Fatal(x)
	}
	if x, _ := q.Pop(100, 1); len(x) != 1 || x[0].ID != "late" {
		t.Fatal(x)
	}
}

func TestPopInvalidLimit(t *testing.T) {
	q := queue(t)
	for _, n := range []int{0, -1} {
		if _, e := q.Pop(0, n); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(n, e)
		}
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "extra"})
	if q.Snapshot().Items[0].ID != "a" || len(q.Snapshot().Items) != 1 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 512, MaxIDBytes: 16})
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
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	if n := len(q.Snapshot().Items); n > 512 {
		t.Fatal(n)
	}
}

func TestConcurrentPopUnique(t *testing.T) {
	q, _ := New(Options{MaxItems: 100, MaxIDBytes: 16})
	for i := 0; i < 100; i++ {
		_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, fmt.Sprintf("id-%d", i), i % 5, 0}}})
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for {
				x, _ := q.Pop(0, 3)
				if len(x) == 0 {
					return
				}
				mu.Lock()
				for _, it := range x {
					seen[it.ID]++
				}
				mu.Unlock()
			}
		}()
	}
	w.Wait()
	if len(seen) != 100 {
		t.Fatal(len(seen))
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatal(id, c)
		}
	}
}
