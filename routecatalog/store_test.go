package routecatalog

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s := store(t)
	good := []string{"a", "0", "-", "_", "abc-def_123", "123456789012"}
	for _, n := range good {
		if _, e := store(t).Apply(Batch{Ops: []Op{{Put, n, []byte("x")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "é", "a/b", "1234567890123"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestValueAndKindValidation(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); ok || e != nil {
		t.Fatal(ok, e)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); e != nil {
		t.Fatal(e)
	}
	r, e = s.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
}

func TestRevisionContinuityAndDelete(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}, {Put, "c", []byte("3")}}})
	if e != nil || r.Revision != 3 || r.Generation != 1 {
		t.Fatal(r, e)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 4 || snap.Generation != 1 || len(snap.Records) != 2 {
		t.Fatal(snap)
	}
	if snap.Records[0].Name != "b" || snap.Records[0].Revision != 2 || snap.Records[1].Name != "c" || snap.Records[1].Revision != 3 {
		t.Fatal(snap.Records)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be deleted")
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("1")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("12345678")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if snap := s.Snapshot(); len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestTotalValueBytesReplace(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 8, MaxTotalValueBytes: 8})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("123456")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestChangedSortedAndFinal(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("1")}, {Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	if e != nil || len(r.Changed) != 3 {
		t.Fatal(r, e)
	}
	names := []string{r.Changed[0].Name, r.Changed[1].Name, r.Changed[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
	}
	if string(r.Changed[0].Value) != "2" || r.Changed[0].Revision != 4 {
		t.Fatal(r.Changed[0])
	}
}

func TestGetSnapshotIsolation(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); e != nil {
		t.Fatal(e)
	}
	r, _, _ := s.Get("a")
	r.Value[0] = 'q'
	snap := s.Snapshot()
	snap.Records[0].Value[1] = 'q'
	r2, ok, _ := s.Get("a")
	if !ok || string(r2.Value) != "xy" {
		t.Fatal(r2)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 4, MaxTotalValueBytes: 512})
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
	if len(snap.Records) != 0 || snap.Generation != 32*21 || snap.NextRevision != 32*20+1 {
		t.Fatal(snap.Generation, snap.NextRevision, len(snap.Records))
	}
}
