package readyqueue410

import (
	"errors"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "A", 1, 0}}},
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},
		{Ops: []Op{{Enqueue, "toolongid9", 1, 0}}},
		{Ops: []Op{{Enqueue, "a", 1, -1}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for i, b := range bad {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	good := Batch{Now: 1, Ops: []Op{{Enqueue, "a-b_c1", 0, 0}, {Cancel, "a-b_c1", 0, 0}}}
	if e := q.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
	if s := q.Stats(); s.Items != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
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
	if s := q.Stats(); s.Now != 5 {
		t.Fatalf("time rolled back incorrectly: %+v", s)
	}
}

func TestFailureRollsBackRevision(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Fails on duplicate "a" after allocating a revision for "b".
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Revision != 2 {
		t.Fatalf("revision not rolled back: got %d want 2", r.Revision)
	}
	if s := q.Stats(); s.NextRevision != 3 || s.Items != 2 {
		t.Fatalf("stats: %+v", s)
	}
}
