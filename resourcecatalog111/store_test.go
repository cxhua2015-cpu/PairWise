package resourcecatalog111

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	cases := []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, -2, 1, 1}, {1, 1, -3, 1}, {1, 1, 1, -4},
	}
	for _, o := range cases {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
}

func TestNameCharset(t *testing.T) {
	s, _ := New(Options{MaxRecords: 16, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	valid := []string{"a", "z9-_", "0", "abc-def_123"}
	for _, n := range valid {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q should be valid: %v", n, e)
		}
	}
	invalid := []string{"", "A", "a b", "a.b", "a/b", "é", "UPPER", "name-that-is-too-long"}
	for _, n := range invalid {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
	}
}

func TestUnknownKindAndValueTooLarge(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of a missing name would be ErrNotFound, but the structural
	// error later in the batch must win because validation runs first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	// Exceeds limits mid-batch but returns within limits by the end.
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12")},
		{Put, "b", []byte("34")},
		{Put, "c", []byte("56")}, // 3 records, 6 bytes mid-batch
		{Delete, "c", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 2 {
		t.Fatal(n)
	}
}

func TestCapacityExceededRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}}})
	before := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("34")}, {Put, "c", []byte("5")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Records) != 1 {
		t.Fatal(got)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	r, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	r, e = s.Apply(Batch{})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	if snap := s.Snapshot(); snap.Generation != 1 || snap.NextRevision != 2 {
		t.Fatal(snap)
	}
}

func TestDeleteMissingAndReput(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "a", []byte("2")}}})
	r, ok, e := s.Get("a")
	if e != nil || !ok || string(r.Value) != "2" || r.Revision != 2 {
		t.Fatal(r, ok, e)
	}
}

func TestGetInvalidNameAndMissing(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")},
	}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte(j)}}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 || snap.Generation != 32*21 {
		t.Fatal(snap.Generation, len(snap.Records))
	}
}

func TestConcurrentRevisionsUnique(t *testing.T) {
	s, _ := New(Options{MaxRecords: 256, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 4096})
	const workers = 16
	revs := make(chan uint64, workers*10)
	var w sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 10; j++ {
				r, e := s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("k%d-%d", i, j), []byte("v")}}})
				if e != nil {
					t.Error(e)
					return
				}
				revs <- r.Revision
			}
		}()
	}
	w.Wait()
	close(revs)
	seen := map[uint64]bool{}
	for r := range revs {
		if seen[r] {
			t.Fatalf("duplicate revision %d", r)
		}
		seen[r] = true
	}
	if len(seen) != workers*10 {
		t.Fatal(len(seen))
	}
}
