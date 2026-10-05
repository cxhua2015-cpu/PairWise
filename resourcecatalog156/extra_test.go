package resourcecatalog156

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

func TestNameCharset(t *testing.T) {
	s := store(t)
	for _, bad := range []string{"", "A", "a b", "a.b", "中文", "a/b", "this-name-is-too-long"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, bad, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", bad, e)
		}
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "ok_name-1", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValueTooLarge(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeState(t *testing.T) {
	s := store(t)
	// Delete of a missing name would be ErrNotFound, but a later invalid op
	// must surface ErrInvalidInput because validation precedes state reads.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("1")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
	// Total value bytes exceeded at batch end.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("1234567")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("generation must not advance on failure")
	}
}

func TestDeleteNotFoundRollbackRevision(t *testing.T) {
	s := store(t)
	_, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "ghost", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	r, e = s.Apply(Batch{})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
}

func TestGetMissingAndInvalid(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	in := []byte("orig")
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", in}}}); e != nil {
		t.Fatal(e)
	}
	in[0] = 'X'
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Y'
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "orig" {
		t.Fatal(string(r.Value))
	}
}

func TestPutOverwriteRevision(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "a", []byte("2")}}})
	if e != nil || x.Revision != 2 || len(x.Changed) != 1 || x.Changed[0].Revision != 2 {
		t.Fatal(x, e)
	}
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "2" || r.Revision != 2 {
		t.Fatal(r)
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
			k := fmt.Sprintf("k-%02d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				snap := s.Snapshot()
				if snap.NextRevision < snap.Generation {
					t.Error("inconsistent snapshot")
				}
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}, {Put, k, []byte("w")}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 16 {
		t.Fatal(len(snap.Records))
	}
	prev := ""
	for _, r := range snap.Records {
		if r.Name <= prev {
			t.Fatal("records not sorted")
		}
		prev = r.Name
	}
}

func TestChangedReflectsDeepCopy(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	x.Changed[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if !reflect.DeepEqual(r.Value, []byte("v")) {
		t.Fatal(string(r.Value))
	}
}
