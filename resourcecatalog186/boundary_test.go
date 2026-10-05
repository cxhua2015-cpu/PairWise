package resourcecatalog186

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
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestNameValidation(t *testing.T) {
	s := store(t)
	bad := []string{"", "A", "has space", "dot.", "中文", "toolongname123"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: got %v", n, e)
		}
	}
	for _, n := range []string{"a", "0", "-", "_", "a-b_c9"} {
		if _, e := store(t).Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestUnknownKindAndValueLimit(t *testing.T) {
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
	// Delete of a missing name would fail with ErrNotFound, but the
	// structural error later in the batch must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Intermediate states exceed both limits; final state fits.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12")},
		{Put, "b", []byte("34")},
		{Put, "c", []byte("56")},
		{Delete, "a", nil},
		{Put, "b", []byte("7")},
		{Put, "c", []byte("8")},
	}})
	if e != nil || len(x.Changed) != 2 {
		t.Fatal(e, x)
	}
}

func TestCapacityExceededRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("34")}, {Put, "c", []byte("5")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("345")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	s := store(t)
	if g := s.Snapshot(); g.Generation != 0 || g.NextRevision != 1 {
		t.Fatal(g)
	}
	// Empty batch: success, no generation bump.
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(e, x)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if x.Generation != 1 || x.Revision != 2 {
		t.Fatal(x)
	}
	if g := s.Snapshot(); g.Generation != 1 || g.NextRevision != 3 {
		t.Fatal(g)
	}
	// Failed batch: no generation or revision movement.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if g := s.Snapshot(); g.Generation != 1 || g.NextRevision != 3 {
		t.Fatal(g)
	}
}

func TestChangedContents(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")},
		{Put, "a", []byte("2")},
		{Delete, "b", nil},
		{Put, "c", []byte("3")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Deleted names are absent; survivors sorted by name with final values.
	if len(x.Changed) != 2 || x.Changed[0].Name != "a" || x.Changed[1].Name != "c" {
		t.Fatal(x.Changed)
	}
	if x.Changed[1].Revision != 3 {
		t.Fatal(x.Changed)
	}
}

func TestGetMissingAndSnapshotIsolation(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'X'
	snap.Records[0].Name = "hacked"
	r, _, _ := s.Get("a")
	if string(r.Value) != "v" || s.Snapshot().Records[0].Name != "a" {
		t.Fatal("snapshot aliases internal state")
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
			k := fmt.Sprintf("k%02d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	g := s.Snapshot()
	if g.NextRevision == 1 || g.Generation == 0 {
		t.Fatal(g)
	}
}

func TestConcurrentSerializedRevisions(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	const n = 16
	revs := make(chan uint64, n)
	var w sync.WaitGroup
	for i := 0; i < n; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
			if e == nil {
				revs <- x.Revision
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
	if len(seen) != n {
		t.Fatalf("got %d revisions, want %d", len(seen), n)
	}
}
