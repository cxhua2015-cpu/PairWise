package readyqueue430

import (
	"errors"
	"sync"
	"testing"
)

func TestBoundaryValidation(t *testing.T) {
	q, err := New(Options{MaxItems: 2, MaxIDBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	cases := []Batch{
		{Now: -1},                                          // negative time
		{Ops: []Op{{Kind: 0, ID: "a"}}},                    // unknown kind
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},               // empty id
		{Ops: []Op{{Kind: Enqueue, ID: "abcde"}}},          // id too long
		{Ops: []Op{{Kind: Enqueue, ID: "A"}}},              // uppercase
		{Ops: []Op{{Kind: Enqueue, ID: "a b"}}},            // space
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}}, // negative ready
		{Ops: []Op{{Kind: Cancel, ID: "a", Priority: 1}}},  // extra field
		{Ops: []Op{{Kind: Cancel, ID: "a", ReadyAt: 1}}},   // extra field
	}
	for i, b := range cases {
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: got %v", i, err)
		}
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: got %v", i, err)
		}
	}
	if err := q.ValidateBatch(Batch{Ops: []Op{{Kind: Enqueue, ID: "a-b", ReadyAt: 9}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{MaxItems: 0, MaxIDBytes: 4}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := New(Options{MaxItems: 1, MaxIDBytes: -1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
}

func TestBoundaryTimeAndCapacity(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Kind: Enqueue, ID: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Kind: Enqueue, ID: "b"}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	// Same-time batch is allowed; over-capacity at end rolls back fully.
	_, err := q.Apply(Batch{Now: 5, Ops: []Op{{Kind: Enqueue, ID: "b"}, {Kind: Enqueue, ID: "c"}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if len(s.Items) != 1 || s.Now != 5 || s.NextRevision != 2 || s.Generation != 1 {
		t.Fatalf("rollback: %+v", s)
	}
	// Duplicate enqueue and missing cancel roll back revision too.
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Kind: Enqueue, ID: "a"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Kind: Cancel, ID: "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if q.Stats().NextRevision != 2 {
		t.Fatal("revision leaked")
	}
	// Empty batch: success, generation unchanged.
	r, err := q.Apply(Batch{Now: 7})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
	if q.Stats().Generation != 1 {
		t.Fatal("empty batch bumped generation")
	}
}

func TestPopBoundary(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	if _, err := q.Pop(-1, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Pop(0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	_, _ = q.Apply(Batch{Now: 1, Ops: []Op{
		{Kind: Enqueue, ID: "a", Priority: 1, ReadyAt: 10},
		{Kind: Enqueue, ID: "b", Priority: 1, ReadyAt: 2},
	}})
	got, err := q.Pop(5, 10)
	if err != nil || len(got) != 1 || got[0].ID != "b" {
		t.Fatal(got, err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for n := 0; n < 20; n++ {
				_, _ = q.Apply(Batch{Now: int64(n), Ops: []Op{{Kind: Enqueue, ID: id, Priority: n}}})
				_, _ = q.Pop(int64(n), 1)
				_ = q.Snapshot()
				_ = q.Stats()
				_, _, _, _ = q.Preview(Batch{Now: int64(n), Ops: []Op{{Kind: Cancel, ID: id}}})
				_ = q.ValidateBatch(Batch{Ops: []Op{{Kind: Enqueue, ID: id}}})
			}
		}()
	}
	w.Wait()
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Pop(1<<40, 1000)
	if len(c.Snapshot().Items) != 0 {
		t.Fatal("clone not fully drained")
	}
}
