package readyqueue230

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, err)
		}
	}
}

func TestValidationBoundaries(t *testing.T) {
	q := queue(t)
	cases := []struct {
		name string
		b    Batch
		err  error
	}{
		{"negative now", Batch{Now: -1}, ErrInvalidInput},
		{"unknown kind", Batch{Ops: []Op{{Kind: 0, ID: "a"}}}, ErrInvalidInput},
		{"kind too large", Batch{Ops: []Op{{Kind: 3, ID: "a"}}}, ErrInvalidInput},
		{"empty id", Batch{Ops: []Op{{Kind: Enqueue}}}, ErrInvalidInput},
		{"uppercase id", Batch{Ops: []Op{{Kind: Enqueue, ID: "A"}}}, ErrInvalidInput},
		{"bad char", Batch{Ops: []Op{{Kind: Enqueue, ID: "a b"}}}, ErrInvalidInput},
		{"id too long", Batch{Ops: []Op{{Kind: Enqueue, ID: "123456789"}}}, ErrInvalidInput},
		{"id at limit", Batch{Ops: []Op{{Kind: Enqueue, ID: "12345678"}}}, nil},
		{"allowed chars", Batch{Ops: []Op{{Kind: Enqueue, ID: "a-0_z"}}}, nil},
		{"negative readyat", Batch{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}}, ErrInvalidInput},
		{"cancel with priority", Batch{Ops: []Op{{Kind: Cancel, ID: "a", Priority: 1}}}, ErrInvalidInput},
		{"cancel with readyat", Batch{Ops: []Op{{Kind: Cancel, ID: "a", ReadyAt: 1}}}, ErrInvalidInput},
	}
	for _, c := range cases {
		if err := q.ValidateBatch(c.b); !errors.Is(err, c.err) {
			t.Errorf("%s: got %v want %v", c.name, err, c.err)
		}
		if err := func() error { _, e := q.Apply(c.b); return e }(); !errors.Is(err, c.err) {
			t.Errorf("apply %s: got %v want %v", c.name, err, c.err)
		}
	}
	if g := q.Snapshot().Generation; g != 2 {
		t.Fatalf("generation=%d, want 2 (only valid non-empty batches)", g)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if s := q.Stats(); s.Now != 5 || s.Items != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestExistsAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	before := q.Snapshot()
	if _, err := q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := q.Snapshot()
	if before.Generation != after.Generation || before.NextRevision != after.NextRevision ||
		before.Now != after.Now || len(after.Items) != 1 {
		t.Fatalf("rollback failed: before=%+v after=%+v", before, after)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Now: 3})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if s := q.Stats(); s.Generation != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestPopOrderAndLimit(t *testing.T) {
	q := queue(t)
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Pop(0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	got, err := q.Pop(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"c", "d", "b"}
	if len(got) != 3 {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "a", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if s := q.Stats(); s.Items != 1 || s.Now != 2 {
		t.Fatalf("original mutated: %+v", s)
	}
	if _, err := c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatalf("clone should reuse id freely: %v", err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("task-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = q.Apply(Batch{Now: int64(j), Ops: []Op{{Enqueue, id, j, 0}}})
				_, _ = q.Pop(int64(j), 1)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
			}
		}()
	}
	wg.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatalf("%+v", s)
	}
	if int(s.Generation) > 16*20 {
		t.Fatalf("generation=%d", s.Generation)
	}
}
