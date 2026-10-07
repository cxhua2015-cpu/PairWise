package readyqueue425

import (
	"errors"
	"sync"
	"testing"
)

func TestOptionsAndStructuralValidation(t *testing.T) {
	if _, err := New(Options{MaxItems: 0, MaxIDBytes: 4}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := New(Options{MaxItems: 1, MaxIDBytes: -1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 4})
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},
		{Ops: []Op{{Kind: Enqueue, ID: "abcde"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "A"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a b"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}},
		{Ops: []Op{{Kind: Cancel, ID: "a", Priority: 1}}},
		{Ops: []Op{{Kind: Cancel, ID: "a", ReadyAt: 1}}},
	}
	for i, b := range bad {
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := q.ValidateBatch(Batch{Ops: []Op{{Kind: Enqueue, ID: "a-b1", ReadyAt: 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestTimeMonotonicAndRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	before := q.Snapshot()
	if _, err := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if got := q.Snapshot(); got.Now != before.Now || got.NextRevision != before.NextRevision || got.Generation != before.Generation {
		t.Fatal("failed batch mutated clocks")
	}
	// Failed enqueue rolls back revision allocation.
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	r, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if err != nil || r.Revision != 2 {
		t.Fatalf("revision leaked: %+v %v", r, err)
	}
}

func TestCapacityCheckedAtEnd(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if len(q.Snapshot().Items) != 0 {
		t.Fatal("capacity failure did not roll back")
	}
	// Cancel-then-enqueue within one batch fits final capacity.
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestPopOrderingAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 9, 10},
		{Enqueue, "c", 9, 3},
		{Enqueue, "d", 9, 3},
	}})
	got, err := q.Pop(3, 10)
	if err != nil || len(got) != 2 || got[0].ID != "c" || got[1].ID != "d" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := q.Pop(0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zzz"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 128, MaxIDBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = q.Apply(Batch{Now: int64(j), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(j), 1)
				_ = q.Snapshot()
				_ = q.Stats()
				_, _, _, _ = q.Preview(Batch{Now: int64(j), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	st := q.Stats()
	if st.Items < 0 || st.Items > 128 {
		t.Fatal(st)
	}
}
