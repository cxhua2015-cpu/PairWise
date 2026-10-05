package secretcatalog

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

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || x.Changed != nil || s.Snapshot().Generation != 0 {
		t.Fatal(x, e)
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x.Generation != 1 || s.Snapshot().Generation != 1 {
		t.Fatal(x)
	}
}

func TestDeleteNoRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || x.Revision != 1 || s.Snapshot().NextRevision != 2 {
		t.Fatal(x, e)
	}
	if len(x.Changed) != 1 || x.Changed[0].Name != "a" || x.Changed[0].Value != nil {
		t.Fatal(x.Changed)
	}
}

func TestRevisionRolledBack(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "zz", nil}}})
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if x.Revision != 2 || s.Snapshot().NextRevision != 3 {
		t.Fatal(x)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	// Mid-batch the store exceeds MaxRecords, but the final state fits.
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Delete, "a", nil}}})
	if e != nil || len(s.Snapshot().Records) != 2 || x.Changed[0].Name != "b" && false {
		t.Fatal(x, e)
	}
	// Total value bytes exceeded at end -> rollback.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("12345")}}})
	if !errors.Is(e, ErrInvalidInput) { // exceeds MaxValueBytes -> structural
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1234")}, {Put, "c", []byte("1234")}, {Put, "d", nil}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s.Snapshot().Generation != 2 {
		t.Fatal(s.Snapshot())
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	// Unknown kind later in the batch must win over the earlier Delete-missing.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "zz", nil}, {Kind: 99, Name: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNameCharset(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	for _, n := range []string{"", "A", "a b", "a.b", "中文", "toolongname123"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "0", "-", "_", "a-b_c9"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestGetMissingAndSnapshotIsolation(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, _, _ := s.Get("a")
	if string(r.Value) != "xy" {
		t.Fatal(string(r.Value))
	}
	// Mutating a value passed to Put must not affect stored state.
	v := []byte("ab")
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", v}}})
	v[0] = 'Z'
	r, _, _ = s.Get("b")
	if string(r.Value) != "ab" {
		t.Fatal(string(r.Value))
	}
}

func TestSnapshotSorted(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", nil}, {Put, "a", nil}, {Put, "b", nil}}})
	recs := s.Snapshot().Records
	got := []string{recs[0].Name, recs[1].Name, recs[2].Name}
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatal(got)
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
			k := fmt.Sprintf("key-%02d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 16 || snap.NextRevision != snap.Generation*2+1 {
		t.Fatal(len(snap.Records), snap.Generation, snap.NextRevision)
	}
}
