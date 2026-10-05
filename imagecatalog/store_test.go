package imagecatalog

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func mustStore(t *testing.T, o Options) *Store {
	t.Helper()
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	cases := []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, -1, 1, 1}, {1, 1, -1, 1}, {1, 1, 1, -1},
		{},
	}
	for _, o := range cases {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, err)
		}
	}
	if _, err := New(valid); err != nil {
		t.Fatal(err)
	}
}

func TestNameBoundaries(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	bad := []string{"", "abcd", "A", "a b", "a.b", "é", "a/b", "Abc"}
	for _, n := range bad {
		if _, err := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput", n)
		}
	}
	for _, n := range []string{"a", "z09", "a-b", "a_b", "---", "___"} {
		if _, err := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	for _, k := range []Kind{0, 3, 255} {
		if _, err := s.Apply(Batch{Ops: []Op{{k, "a", nil}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("kind %d: want ErrInvalidInput", k)
		}
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("delete with value: want ErrInvalidInput")
	}
}

func TestValueTooLarge(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xyz")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	// Invalid op comes after a delete of a missing name; structural
	// validation must win over the state-dependent ErrNotFound.
	_, err := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("v")}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
}

func TestDeleteMissingAndRollback(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	r1, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	_, err = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("w")}, {Delete, "ghost", nil}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	r2, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("w")}}})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Generation != r1.Generation+1 || r2.Revision != r1.Revision+1 {
		t.Fatalf("generation/revision leaked: %+v after %+v", r2, r1)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	// Overwrite in the same batch: peak total is fine, count stays 1.
	s := mustStore(t, Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "a", []byte("2")}}}); err != nil {
		t.Fatal(err)
	}
	// Delete-then-put within one batch respects the final record count.
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("3")}}}); err != nil {
		t.Fatal(err)
	}
	// Final count exceeded -> ErrCapacity and full rollback.
	before := s.Snapshot()
	_, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("4")}, {Put, "d", []byte("5")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("want ErrCapacity, got %v", err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestTotalValueBytesCapacity(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("22")}}}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("want ErrCapacity, got %v", err)
	}
	// Shrinking values frees budget.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "c", []byte("3")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	r0, err := s.Apply(Batch{})
	if err != nil {
		t.Fatal(err)
	}
	if r0.Generation != 0 || r0.Revision != 0 || r0.Changed != nil {
		t.Fatalf("unexpected empty-batch result: %+v", r0)
	}
	r1, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.Apply(Batch{Ops: []Op{}})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Generation != r1.Generation || r2.Revision != r1.Revision {
		t.Fatalf("empty batch changed counters: %+v vs %+v", r2, r1)
	}
}

func TestGenerationIncrementsOncePerBatch(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	r, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatalf("got %+v", r)
	}
	if snap := s.Snapshot(); snap.Generation != 1 || snap.NextRevision != 3 {
		t.Fatalf("got %+v", snap)
	}
}

func TestChangedContents(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "c", nil},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[1].Name != "b" {
		t.Fatalf("changed not sorted/final: %+v", r.Changed)
	}
	if r.Changed[0].Revision != 2 || r.Changed[1].Revision != 1 {
		t.Fatalf("revisions: %+v", r.Changed)
	}
	// Mutating Changed must not affect the store.
	r.Changed[0].Value[0] = 'X'
	rec, ok, err := s.Get("a")
	if err != nil || !ok || string(rec.Value) != "2" {
		t.Fatalf("store aliased Changed: %q %v %v", rec.Value, ok, err)
	}
}

func TestGetValidationAndMissing(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, _, err := s.Get("bad!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
	if _, ok, err := s.Get("nope"); err != nil || ok {
		t.Fatalf("missing key: ok=%v err=%v", ok, err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatalf("snapshot not sorted: %+v", snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	rec, _, _ := s.Get("a")
	if !bytes.Equal(rec.Value, []byte("2")) {
		t.Fatalf("snapshot aliases store: %q", rec.Value)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 4096})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("key-%02d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, name, nil}, {Put, name, []byte("w")}}})
			}
		}()
	}
	wg.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 16 {
		t.Fatalf("records=%d", len(snap.Records))
	}
	if snap.Generation == 0 || snap.NextRevision <= 1 {
		t.Fatalf("counters not advanced: %+v", snap)
	}
}

func TestConcurrentSnapshotConsistency(t *testing.T) {
	s := mustStore(t, Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4096})
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("y")}}})
		}
	}()
	for i := 0; i < 200; i++ {
		snap := s.Snapshot()
		var revA, revB uint64
		for _, r := range snap.Records {
			if r.Name == "a" {
				revA = r.Revision
			}
			if r.Name == "b" {
				revB = r.Revision
			}
		}
		if (revA == 0) != (revB == 0) {
			t.Fatalf("torn snapshot: a=%d b=%d", revA, revB)
		}
		if revA != 0 && revB != revA+1 {
			t.Fatalf("torn snapshot: a=%d b=%d", revA, revB)
		}
	}
	close(stop)
	wg.Wait()
}
