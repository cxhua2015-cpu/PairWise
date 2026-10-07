package metacatalog356

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
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameCharsetAndLength(t *testing.T) {
	s := store(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "toolongname123"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: got %v", n, e)
		}
	}
	good := []string{"a", "0", "-"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: got %v", n, e)
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
	if len(s.Snapshot().Records) != 0 {
		t.Fatal("state mutated by invalid batch")
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of missing name would fail with ErrNotFound, but the later
	// structural error must win because validation precedes state reads.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	// Transient overflow within the batch is fine if the final state fits.
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 8})
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1234")}, // total 4
		{Put, "b", []byte("1234")}, // total 8
		{Put, "c", []byte("1234")}, // total 12 > 8, 3 records > 2 (transient)
		{Delete, "a", nil},
		{Delete, "b", nil}, // final: 1 record, total 4
	}})
	if e != nil {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 1 {
		t.Fatal(n)
	}
}

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 8})
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1234")}, {Put, "c", []byte("1234")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state not rolled back")
	}
	// Total-bytes overflow also rolls back.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("12345678")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if g := s.Snapshot().Generation; g != x.Generation {
		t.Fatal("generation changed on failure")
	}
}

func TestDeleteNotFoundAndReput(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Delete then re-put the same name in one batch works.
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "a", []byte("2")}}})
	if e != nil || x.Revision != 2 {
		t.Fatal(e, x)
	}
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "2" || r.Revision != 2 {
		t.Fatal(r, ok)
	}
}

func TestEmptyBatchNoGenerationBump(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("generation bumped by empty batch")
	}
}

func TestSnapshotOrderingAndIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if snap.NextRevision != 4 || snap.Generation != 1 {
		t.Fatal(snap)
	}
	names := []string{snap.Records[0].Name, snap.Records[1].Name, snap.Records[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
	}
	// Mutating the snapshot must not affect the store.
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases store memory")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 16, MaxTotalValueBytes: 1 << 20})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte(j)}}}})
				r, ok, _ := s.Get(k)
				if ok {
					r.Value[0] = 0xff // mutate copy; must not corrupt store
				}
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}, {Put, k, []byte("end")}}})
		}()
	}
	w.Wait()
	if n := len(s.Snapshot().Records); n != 32 {
		t.Fatal(n)
	}
}
