package expirytable439

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, err := New(o); err != ErrInvalidOptions {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestKeyAndKindValidation(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 4})
	bad := []Op{
		{Put, "", 5},
		{Put, "Upper", 5},
		{Put, "toolong", 5},
		{Put, "bad key", 5},
		{Put, "a", -1},
		{Put, "a", 0},   // ExpiresAt <= Now(0)
		{Touch, "a", 0}, // ExpiresAt <= Now(0)
		{Kind(0), "a", 5},
		{Kind(9), "a", 5},
	}
	for _, op := range bad {
		if err := x.ValidateBatch(Batch{Ops: []Op{op}}); err != ErrInvalidInput {
			t.Fatalf("op %+v: %v", op, err)
		}
		if _, err := x.Apply(Batch{Ops: []Op{op}}); err != ErrInvalidInput {
			t.Fatalf("apply op %+v: %v", op, err)
		}
	}
	for _, k := range []string{"a", "z9-_", "abcd"} {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 5}}}); err != nil {
			t.Fatalf("key %q: %v", k, err)
		}
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Touch, "ghost", 5}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Delete, "ghost", 0}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	// Capacity failure must roll back evictions, time, and revisions.
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 3}, {Put, "b", 10}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// Evicts a (ExpiresAt 3 <= 4), then b,c,d = 3 entries exceed MaxEntries 2.
	_, err := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if err != ErrCapacity {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision || len(after.Entries) != 2 {
		t.Fatalf("no rollback: %+v -> %+v", before, after)
	}
	// Failed batch must not consume revisions. At Now=4 the candidate
	// eviction removes a (ExpiresAt 3 <= 4); deleting b then inserting
	// c and d fits the capacity of 2.
	r, err := x.Apply(Batch{Now: 4, Ops: []Op{{Delete, "b", 0}, {Put, "c", 9}, {Put, "d", 9}}})
	if err != nil || r.Revision != 4 {
		t.Fatalf("revision leaked: %+v %v", r, err)
	}
	if got := x.Snapshot().Entries; len(got) != 2 || got[0].Key != "c" {
		t.Fatalf("entries: %+v", got)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	r, err := x.Apply(Batch{Now: 3})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	if x.Snapshot().Now != 3 {
		t.Fatal("empty batch should still advance time")
	}
	if _, err := x.Apply(Batch{Now: 2}); err != ErrTime {
		t.Fatal(err)
	}
	r, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}}})
	r2, _ := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Touch, "a", 10}}})
	if r.Generation != 1 || r2.Generation != 2 {
		t.Fatalf("generations: %d %d", r.Generation, r2.Generation)
	}
}

func TestExpireBoundaryAndClock(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	gone, err := x.Expire(5) // closed interval: ExpiresAt <= now
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatalf("gone=%v err=%v", gone, err)
	}
	if _, err := x.Expire(4); err != ErrTime {
		t.Fatal(err)
	}
	if _, err := x.Expire(-1); err != ErrTime {
		t.Fatal(err)
	}
	if s := x.Stats(); s.Now != 5 || s.Entries != 1 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for n := int64(1); n <= 10; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				_ = x.ValidateBatch(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_, _, _, _ = x.Preview(Batch{Now: n, Ops: []Op{{Touch, k, n + 300}}})
			}
		}()
	}
	w.Wait()
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(c.Snapshot().Entries); got != 32 {
		t.Fatal(got)
	}
	// Clone ownership: mutating the clone leaves the original untouched.
	_, _ = c.Expire(1000)
	if len(x.Snapshot().Entries) != 32 || len(c.Snapshot().Entries) != 0 {
		t.Fatal("clone aliases original")
	}
}

func TestPreviewErrorParity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}})
	cases := []struct {
		b    Batch
		want error
	}{
		{Batch{Now: 1, Ops: []Op{{Kind(0), "a", 9}}}, ErrInvalidInput},
		{Batch{Now: 0}, ErrTime},
		{Batch{Now: 1, Ops: []Op{{Touch, "zz", 9}}}, ErrNotFound},
		{Batch{Now: 1, Ops: []Op{{Put, "b", 9}}}, ErrCapacity},
	}
	for _, tc := range cases {
		_, aerr := x.Apply(tc.b)
		_, _, _, perr := x.Preview(tc.b)
		if !errors.Is(perr, tc.want) || !errors.Is(aerr, tc.want) || aerr != perr {
			t.Fatalf("%+v: apply=%v preview=%v want=%v", tc.b, aerr, perr, tc.want)
		}
	}
}
