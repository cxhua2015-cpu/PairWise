package resourcecatalog101

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, 1, 1, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	s := store(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "toolongname123"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	good := []string{"a", "0", "-", "_", "a-b_c9"}
	s, _ = New(Options{MaxRecords: 16, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndValueTooLong(t *testing.T) {
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
	// Delete of missing name would be ErrNotFound, but structural error must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	snap := s.Snapshot()
	if snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if x.Generation != 1 || s.Snapshot().Generation != 1 {
		t.Fatal(x)
	}
	// Failed batch leaves generation unchanged.
	_, _ = s.Apply(Batch{Ops: []Op{{Delete, "zz", nil}}})
	if s.Snapshot().Generation != 1 {
		t.Fatal(s.Snapshot())
	}
}

func TestDeleteDoesNotAllocateRevision(t *testing.T) {
	s := store(t)
	x1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	x2, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	x3, _ := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if x1.Revision != 1 || x2.Revision != 1 || x3.Revision != 2 {
		t.Fatal(x1, x2, x3)
	}
	if s.Snapshot().NextRevision != 3 {
		t.Fatal(s.Snapshot())
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	// Transiently exceeds MaxRecords mid-batch but ends within limits: OK.
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 2})
	_, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Put, "b", []byte("w")}, {Delete, "a", nil}}})
	if e != nil {
		t.Fatal(e)
	}
	// Ends over MaxRecords: ErrCapacity with full rollback.
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}, {Put, "d", []byte("y")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	// Ends over total value bytes: ErrCapacity with rollback.
	_, e = s.Apply(Batch{Ops: []Op{{Delete, "b", nil}, {Put, "c", []byte("1234")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestRollbackRestoresRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "zz", nil}}})
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if x.Revision != 2 {
		t.Fatal(x)
	}
}

func TestGetNotFoundAndInvalid(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("BAD!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal(r)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%03d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte('a' + j%26)}}}})
				if j%3 == 0 {
					_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
				}
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) > 128 {
		t.Fatal(len(snap.Records))
	}
	total := 0
	for _, r := range snap.Records {
		total += len(r.Value)
	}
	if total > 512 {
		t.Fatal(total)
	}
}

func TestConcurrentPutSameKey(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			_, _ = s.Apply(Batch{Ops: []Op{{Put, "same", []byte("v")}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 1 || snap.Records[0].Revision != snap.NextRevision-1 || snap.Generation != 16 {
		t.Fatal(snap)
	}
}
