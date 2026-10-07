package readyqueue430

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestValidateBatchStructural(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},
		{Ops: []Op{{Kind: Enqueue, ID: "ABCDEFGH"}}},        // 8 ascii but uppercase
		{Ops: []Op{{Kind: Enqueue, ID: "toolongid1"}}},      // > MaxIDBytes
		{Ops: []Op{{Kind: Enqueue, ID: "bad id"}}},          // space
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}},  // negative ready
		{Ops: []Op{{Kind: Cancel, ID: "a", Priority: 1}}},   // cancel with priority
		{Ops: []Op{{Kind: Cancel, ID: "a", ReadyAt: 1}}},    // cancel with readyat
	}
	for i, b := range cases {
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := q.ValidateBatch(Batch{Ops: []Op{{Kind: Enqueue, ID: "ok_id-1", Priority: -5}}}); err != nil {
		t.Fatal(err)
	}
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Now: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 0 || q.Snapshot().Now != 3 {
		t.Fatalf("empty batch: %+v %+v", r, q.Snapshot())
	}
	if _, err := q.Apply(Batch{Now: 2}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestDuplicateAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 2, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatalf("rollback left %d items", n)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if len(s.Items) != 0 || s.NextRevision != 1 || s.Generation != 0 {
		t.Fatalf("capacity rollback: %+v", s)
	}
}

func TestPopOrderAndIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 5, 1}, {Enqueue, "c", 5, 0}, {Enqueue, "d", 5, 0},
	}})
	got, err := q.Pop(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"c", "d", "b"}
	for i, it := range got {
		if it.ID != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	got[0].ID = "mutated"
	if q.Snapshot().Items[0].ID == "mutated" {
		t.Fatal("pop result aliases state")
	}
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID == "mutated" {
		t.Fatal("snapshot aliases state")
	}
	if _, err := q.Pop(0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestConcurrentMix(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("id-%d-%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Snapshot()
				_ = q.Stats()
				_, _, _, _ = q.Preview(Batch{Now: int64(i), Ops: []Op{{Kind: Cancel, ID: id}}})
				if i%10 == 0 {
					_, _ = q.Clone()
				}
			}
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	st := q.Stats()
	if st.Items != len(s.Items) || st.Generation != s.Generation || st.NextRevision != s.NextRevision {
		t.Fatalf("inconsistent final state: %+v vs %+v", st, s)
	}
}
