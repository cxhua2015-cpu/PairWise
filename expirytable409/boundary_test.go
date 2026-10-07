package expirytable409

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

func TestStructuralValidation(t *testing.T) {
	x := table(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Upper", 1}}},
		{Ops: []Op{{Put, "a b", 1}}},
		{Ops: []Op{{Put, "toolongkey", 1}}},
		{Ops: []Op{{Put, "a", -1}}},
		{Ops: []Op{{Put, "a", 0}}},           // ExpiresAt <= Now
		{Now: 2, Ops: []Op{{Touch, "a", 2}}}, // closed boundary
		{Ops: []Op{{Delete, "ok-key_1", 0}}}, // valid structurally
		{Ops: []Op{{Put, "ok-key_1", 1}}},    // valid
	}
	for i, b := range cases {
		wantInvalid := i < 10
		e := x.ValidateBatch(b)
		if wantInvalid && !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: want ErrInvalidInput, got %v", i, e)
		}
		if !wantInvalid && e != nil {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if s := x.Stats(); s.Entries != 0 || s.Generation != 0 {
		t.Fatalf("ValidateBatch mutated state: %+v", s)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// "a" expires at Now=5, but the batch then overflows capacity: full rollback.
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("no rollback: %+v", after)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Error mid-batch: earlier Put must roll back too.
	_, e := x.Apply(Batch{Ops: []Op{{Put, "b", 9}, {Delete, "ghost", 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.NextRevision != before.NextRevision || len(after.Entries) != 1 {
		t.Fatalf("revision/state leaked: %+v", after)
	}
}

func TestMonotonicTimeAndGeneration(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 3}); e != nil {
		t.Fatal(e)
	}
	if g := x.Snapshot().Generation; g != 0 {
		t.Fatalf("empty batch bumped generation: %d", g)
	}
	if _, e := x.Apply(Batch{Now: 2}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(2); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExpireClosedBoundaryAndIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 4}, {Put, "b", 5}, {Put, "c", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "c" {
		t.Fatal("returned slice aliases state")
	}
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 0
	if x.Snapshot().Entries[0].ExpiresAt != 6 {
		t.Fatal("snapshot aliases state")
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
			for n := 0; n < 20; n++ {
				_, _ = x.Apply(Batch{Ops: []Op{{Put, k, 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	st := x.Stats()
	if st.Entries != 32 {
		t.Fatalf("%+v", st)
	}
	if _, err := x.Expire(50); err != nil {
		t.Fatal(err)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(Batch{Now: 60, Ops: []Op{{Delete, "key-0", 0}}}); err != nil {
		t.Fatal(err)
	}
	if x.Snapshot().Now != 50 || len(x.Snapshot().Entries) != 32 {
		t.Fatal("clone shares state with original")
	}
}
