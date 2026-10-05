package tokenvault

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {3, 0}, {-1, 8}, {3, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	x := table(t)
	bad := []Op{
		{Kind: 0, Key: "a", ExpiresAt: 1},
		{Kind: 99, Key: "a", ExpiresAt: 1},
		{Kind: Put, Key: "", ExpiresAt: 1},
		{Kind: Put, Key: "A", ExpiresAt: 1},
		{Kind: Put, Key: "a b", ExpiresAt: 1},
		{Kind: Put, Key: "toolongkey", ExpiresAt: 1},
		{Kind: Put, Key: "a", ExpiresAt: -1},
		{Kind: Touch, Key: "a", ExpiresAt: -1},
		{Kind: Delete, Key: "bad!"},
	}
	for _, op := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Generation != 0 || len(s.Entries) != 0 {
		t.Fatalf("state mutated: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}, {Put, "c", 4}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// "a" would expire at Now=2, but the batch still overflows.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "d", 9}, {Put, "e", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != 3 {
		t.Fatalf("not rolled back: %+v -> %+v", before, after)
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	for _, ops := range [][]Op{
		{{Touch, "missing", 3}},
		{{Delete, "missing", 0}},
		{{Put, "b", 9}, {Touch, "missing", 3}},
	} {
		if _, e := x.Apply(Batch{Now: 2, Ops: ops}); !errors.Is(e, ErrNotFound) {
			t.Fatalf("%v: %v", ops, e)
		}
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision || len(after.Entries) != 1 {
		t.Fatalf("not rolled back: %+v", after)
	}
}

func TestExpireBoundaryAndTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(0); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatalf("closed boundary: %v %v", gone, e)
	}
	if s := x.Snapshot(); s.Now != 5 || len(s.Entries) != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Now != 3 {
		t.Fatal("empty batch should still advance time")
	}
	r, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.NextRevision != 3 {
		t.Fatalf("%+v", s)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%d", i)
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_, _ = x.Expire(n)
				s := x.Snapshot()
				for _, e := range s.Entries {
					if e.ExpiresAt < s.Now {
						t.Errorf("snapshot holds expired entry: %+v now=%d", e, s.Now)
					}
				}
			}
			_, _ = x.Expire(1000)
		}()
	}
	w.Wait()
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatalf("leftover entries: %d", n)
	}
}
