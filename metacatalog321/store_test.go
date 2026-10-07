package metacatalog321

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
	s, _ := New(Options{MaxRecords: 16, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	ok := []string{"a", "abc-123_x", "0", "z"}
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "é", "a/b", "this-name-is-way-too-long"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValueTooLarge(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
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
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x.Generation != 1 {
		t.Fatal(x)
	}
	y, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if y.Generation != 2 || y.Revision != 2 {
		t.Fatal(y)
	}
}

func TestDeleteMissingRollbackRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Put, "c", []byte("z")}, {Delete, "nope", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.Generation != 1 || snap.NextRevision != 2 || len(snap.Records) != 1 {
		t.Fatal(snap)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	// Intermediate state exceeds MaxRecords, final state does not: must succeed.
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	_, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "a", nil}}})
	if e != nil {
		t.Fatal(e)
	}
	// Final state exceeds MaxRecords: must fail and roll back.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(s.Snapshot().Records) != 2 {
		t.Fatal(s.Snapshot())
	}
}

func TestTotalValueCapacity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 10, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}}}); e != nil {
		t.Fatal(e)
	}
	// Overwrite with smaller value frees budget; net stays within limit.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "c", []byte("2")}}}); e != nil {
		t.Fatal(e)
	}
	_, e := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("345")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if _, ok, _ := s.Get("d"); ok {
		t.Fatal("d should be rolled back")
	}
}

func TestGetInvalidAndMissing(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); ok || e != nil {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot mutation leaked into store")
	}
}

func TestChangedDeleteEntry(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if e != nil || len(x.Changed) != 2 || x.Changed[0].Name != "a" || x.Changed[0].Value != nil || x.Changed[1].Name != "b" {
		t.Fatal(e, x)
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
				r, ok, _ := s.Get(k)
				if ok {
					r.Value[0] = 0xff // mutate copy; must not corrupt store
				}
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
	s, _ := New(Options{MaxRecords: 256, MaxNameBytes: 16, MaxValueBytes: 4, MaxTotalValueBytes: 4096})
	const n = 16
	var w sync.WaitGroup
	seen := make([][]uint64, n)
	for i := 0; i < n; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 10; j++ {
				x, e := s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("k%d-%d", i, j), []byte("v")}}})
				if e != nil {
					t.Error(e)
					return
				}
				seen[i] = append(seen[i], x.Revision)
			}
		}()
	}
	w.Wait()
	uniq := map[uint64]bool{}
	for _, rs := range seen {
		for _, r := range rs {
			if uniq[r] {
				t.Fatal("duplicate revision", r)
			}
			uniq[r] = true
		}
	}
	if len(uniq) != n*10 || s.Snapshot().NextRevision != uint64(n*10)+1 {
		t.Fatal(len(uniq), s.Snapshot().NextRevision)
	}
	if !reflect.DeepEqual(len(s.Snapshot().Records), n*10) {
		t.Fatal(len(s.Snapshot().Records))
	}
}
