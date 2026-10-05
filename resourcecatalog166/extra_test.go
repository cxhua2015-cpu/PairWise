package resourcecatalog166

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, 1, 1, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	if e != nil {
		t.Fatal(e)
	}
	// Valid: exactly at limit, all allowed char classes.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "z_0", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	// Invalid: empty, too long, uppercase, non-ASCII, punctuation.
	for _, n := range []string{"", "abcd", "ABC", "é", "a b", "a.b", "a/b"} {
		if _, e = s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
		if _, _, e = s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: want ErrInvalidInput, got %v", n, e)
		}
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	s := store(t)
	for _, op := range []Op{
		{Kind: 0, Name: "a"},
		{Kind: 99, Name: "a"},
		{Kind: Delete, Name: "a", Value: []byte("x")},
	} {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: want ErrInvalidInput, got %v", op, e)
		}
	}
}

func TestValidationPrecedesStateRead(t *testing.T) {
	s := store(t)
	// Delete of a missing name would fail with ErrNotFound, but a later
	// structural error must win because validation runs first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValueByteLimits(t *testing.T) {
	s := store(t) // MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	// Over-capacity mid-batch is fine if the batch ends within limits.
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12")},
		{Put, "b", []byte("34")},
		{Put, "c", []byte("56")}, // 3 records mid-batch
		{Delete, "c", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 2 {
		t.Fatal(n)
	}
	// Ending over capacity fails and rolls back.
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("78")}, {Put, "d", []byte("90")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Records) != 2 {
		t.Fatal("state changed after failed batch", got)
	}
}

func TestTotalValueBytesLimit(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}}}); e != nil {
		t.Fatal(e)
	}
	// Replacing a value frees its bytes for the total.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}, {Delete, "b", nil}}}); e != nil {
		t.Fatal(e)
	}
	_, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("5")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r1, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	if e != nil {
		t.Fatal(e)
	}
	r2, e := s.Apply(Batch{})
	if e != nil {
		t.Fatal(e)
	}
	if r2.Generation != r1.Generation || r2.Revision != r1.Revision || len(r2.Changed) != 0 {
		t.Fatal(r1, r2)
	}
	if g := s.Snapshot().Generation; g != r1.Generation {
		t.Fatal(g)
	}
}

func TestDeleteRollbackRestoresRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Delete, "nope", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// The failed Put's revision must not be consumed.
	r, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(e, r)
	}
	rec, ok, _ := s.Get("b")
	if !ok || rec.Revision != 2 {
		t.Fatal(rec, ok)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'z'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 16, MaxTotalValueBytes: 4096})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	// Revisions are never reused: total puts = 32*20, plus deletes interleaved.
	if got := s.Snapshot().NextRevision; got != 32*20+1 {
		t.Fatal(got)
	}
}
