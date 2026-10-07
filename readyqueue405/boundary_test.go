package readyqueue405

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {4, 0}, {-1, 8}, {4, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestValidateBatchStructural(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "Upper", 1, 0}}},
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},
		{Ops: []Op{{Enqueue, "toolongid", 1, 0}}}, // 9 > MaxIDBytes 8
		{Ops: []Op{{Enqueue, "a", 1, -1}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for i, b := range cases {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	ok := Batch{Ops: []Op{{Enqueue, "ok-id_1", 1, 0}, {Cancel, "ok-id_1", 0, 0}}}
	if e := q.ValidateBatch(ok); e != nil {
		t.Fatal(e)
	}
	// ValidateBatch must not mutate state.
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatalf("validation leaked state: %+v", s)
	}
}

func TestTimeMonotonicAndRollback(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// Failed batch must roll back time, state and revision.
	if _, e := q.Apply(Batch{Now: 6, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.Now != 5 || s.Generation != 1 || s.NextRevision != 2 || len(s.Items) != 1 {
		t.Fatalf("rollback: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("capacity rollback: %+v", s)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 10})
	if e != nil {
		t.Fatal(e)
	}
	if r.Generation != 0 || q.Snapshot().Generation != 0 {
		t.Fatalf("empty batch changed generation: %+v", r)
	}
}

func TestRevisionMonotonicAcrossBatches(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	r2, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if r1.Revision != 2 || r2.Revision != 3 {
		t.Fatalf("revisions: %d %d", r1.Revision, r2.Revision)
	}
}

func TestPopOrderAndAtomicity(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 0},
		{Enqueue, "y", 5, 1},
		{Enqueue, "z", 5, 0},
		{Enqueue, "w", 5, 0},
	}})
	got, e := q.Pop(0, 10)
	if e != nil || len(got) != 3 {
		t.Fatal(e, got)
	}
	if got[0].ID != "w" || got[1].ID != "z" || got[2].ID != "x" {
		t.Fatalf("order: %v", got)
	}
	rest, _ := q.Pop(10, 10)
	if len(rest) != 1 || rest[0].ID != "y" {
		t.Fatalf("remaining: %v", rest)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestPopInvalid(t *testing.T) {
	q := queue(t)
	if _, e := q.Pop(-1, 1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, -2); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	// Clocks preserved.
	if s := c.Snapshot(); s.Now != 3 || s.Generation != 1 || s.NextRevision != 2 {
		t.Fatalf("clone clocks: %+v", s)
	}
	// Mutations on either side are invisible to the other.
	_, _ = c.Apply(Batch{Now: 4, Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "b", 1, 0}}})
	_, _ = q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "c", 1, 0}}})
	qs, cs := q.Snapshot(), c.Snapshot()
	if len(qs.Items) != 2 || len(cs.Items) != 1 || cs.Items[0].ID != "b" {
		t.Fatalf("q=%+v c=%+v", qs, cs)
	}
}

func TestConcurrentMix(t *testing.T) {
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
				_, _ = q.Pop(int64(i), 4)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
				if i%10 == 0 {
					_, _ = q.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items != len(q.Snapshot().Items) {
		t.Fatalf("stats/snapshot mismatch: %d", s.Items)
	}
}

func TestConcurrentCancelExactlyOneWins(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	var w sync.WaitGroup
	var mu sync.Mutex
	ok, notFound := 0, 0
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			_, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
			mu.Lock()
			switch {
			case e == nil:
				ok++
			case errors.Is(e, ErrNotFound):
				notFound++
			}
			mu.Unlock()
		}()
	}
	w.Wait()
	if ok != 1 || notFound != 15 {
		t.Fatalf("ok=%d notFound=%d", ok, notFound)
	}
}
