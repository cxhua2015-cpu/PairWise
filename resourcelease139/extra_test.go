package resourcelease139

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptionsAndInput(t *testing.T) {
	if _, e := New(Options{MaxEntries: 0, MaxKeyBytes: 8}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxEntries: 1, MaxKeyBytes: 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x := table(t)
	for _, op := range []Op{
		{Kind: 0, Key: "a", ExpiresAt: 1},
		{Kind: 99, Key: "a", ExpiresAt: 1},
		{Kind: Put, Key: "", ExpiresAt: 1},
		{Kind: Put, Key: "Bad", ExpiresAt: 1},
		{Kind: Put, Key: "a b", ExpiresAt: 1},
		{Kind: Put, Key: "toolongkey", ExpiresAt: 1},
		{Kind: Put, Key: "a", ExpiresAt: -1},
	} {
		if _, e := x.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if s := x.Snapshot(); s.Generation != 0 || len(s.Entries) != 0 {
		t.Fatal(s)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Touch, "ghost", 9}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}, {Put, "c", 4}}})
	before := x.Snapshot()
	// "a" expires at 2 <= Now, freeing a slot, but 4 puts still overflow 3.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "d", 9}, {Put, "e", 9}, {Put, "f", 9}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal(e, x.Snapshot())
	}
	// Same batch fits once two entries expire.
	r, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "d", 9}, {Put, "e", 9}, {Put, "f", 9}}})
	if e != nil || r.Generation != 2 || len(x.Snapshot().Entries) != 3 {
		t.Fatal(e, r)
	}
}

func TestExpireBoundaryAndTime(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if _, e = x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "c", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	p, _ := NewPolicy(2, []string{"alice"})
	c, _ := NewCoordinator(x, p)
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			key := fmt.Sprintf("k-%d", i)
			_, _ = c.Apply("alice", Batch{Now: int64(i), Ops: []Op{{Put, key, 100}}})
			_, _ = c.Apply("mallory", Batch{Now: int64(i), Ops: []Op{{Put, key, 100}}})
			_, _ = x.Expire(int64(i))
			_ = x.Snapshot()
			_ = c.Decisions()
			_ = p.ReplaceActors([]string{"alice", "bob"})
		}()
	}
	w.Wait()
	ds := c.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("gap at %d: %+v", i, d)
		}
	}
	if len(ds) != 64 {
		t.Fatal(len(ds))
	}
}

func TestPolicyReplaceAtomic(t *testing.T) {
	p, err := NewPolicy(1, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	if e := p.Authorize("alice", 1); e != nil {
		t.Fatal(e)
	}
	if e := p.Authorize("alice", 2); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.ReplaceActors([]string{"bob"}); e != nil {
		t.Fatal(e)
	}
	if e := p.Authorize("alice", 1); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.Authorize("bob", 1); e != nil {
		t.Fatal(e)
	}
	if _, e := NewPolicy(0, nil); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := NewCoordinator(nil, p); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
}
