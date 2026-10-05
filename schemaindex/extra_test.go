package schemaindex

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
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	ok := []string{"a", "abc", "a-1", "_", "z9_"}
	for _, n := range ok {
		if _, e = s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "abcd", "A", "a b", "a.b", "é", "a/b"}
	for _, n := range bad {
		if _, e = s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestValueLengthBoundary(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("ab")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("abc")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", nil}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	b := s.Snapshot()
	for _, k := range []Kind{0, 3, 255} {
		if _, e := s.Apply(Batch{Ops: []Op{{k, "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: got %v", k, e)
		}
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after invalid kind")
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	// Delete of a missing name would fail with ErrNotFound, but the later
	// structural error must win because validation precedes state reads.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "zz", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	x, e = s.Apply(Batch{Ops: nil})
	if e != nil || x.Generation != 1 {
		t.Fatal(e, x)
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if e != nil || x.Generation != 1 || s.Snapshot().Generation != 1 {
		t.Fatal(e, x)
	}
}

func TestRevisionContinuity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}, {Put, "c", []byte("3")}}})
	if x.Revision != 3 {
		t.Fatal(x.Revision)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 4 {
		t.Fatal(snap.NextRevision)
	}
	for _, r := range snap.Records {
		if r.Name == "b" && r.Revision != 2 || r.Name == "c" && r.Revision != 3 {
			t.Fatal(r)
		}
	}
}

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("22")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	// Final record count exceeded.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("33")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Final total value bytes exceeded.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3333")}, {Put, "d", []byte("4444")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Intermediate overflow that resolves by batch end must succeed.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Delete, "a", nil}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteMissingRollbackRevision(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("revision/generation not rolled back")
	}
}

func TestChangedDedupSorted(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	x, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "a", nil}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(x.Changed) != 1 || x.Changed[0].Name != "c" || x.Changed[0].Revision != 3 || string(x.Changed[0].Value) != "3" {
		t.Fatal(x.Changed)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'x'
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestGetInvalidAndMissing(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("none"); e != nil || ok {
		t.Fatal(e, ok)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte(j)}}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}, {Put, k, []byte{byte(j)}}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	// Revisions must be unique and contiguous from 1.
	seen := make(map[uint64]bool)
	for _, r := range snap.Records {
		if seen[r.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[r.Revision] = true
	}
	// 32 goroutines * 20 iterations * 2 applies, each with exactly one Put.
	if snap.NextRevision != 32*20*2+1 {
		t.Fatal(snap.NextRevision)
	}
}
