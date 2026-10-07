package metacatalog316

import (
	"errors"
	"fmt"
	"reflect"
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

func TestNameCharsetAndLength(t *testing.T) {
	s := store(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "abc!", "this-name-is-way-too-long"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
	}
	good := []string{"a", "0", "-", "_", "a-b_c-9", "abcdefghijkl"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}, {Delete, n, nil}}}); e != nil {
			t.Fatalf("name %q: unexpected error %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	for _, k := range []Kind{0, 3, 255} {
		if _, e := s.Apply(Batch{Ops: []Op{{k, "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: want ErrInvalidInput, got %v", k, e)
		}
	}
}

func TestValueTooLarge(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestValidationBeforeState(t *testing.T) {
	s := store(t)
	// Second op is structurally invalid; first op deletes a missing name.
	// Structural validation must win over the stateful ErrNotFound.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", e)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Intermediate state exceeds both limits, final state fits.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
		{Delete, "b", nil},
	}})
	if e != nil || len(x.Changed) != 1 {
		t.Fatal(e, x)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 1 || snap.Records[0].Name != "c" {
		t.Fatal(snap)
	}
}

func TestCapacityExceeded(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")}, {Put, "b", []byte("2")},
		{Put, "c", []byte("3")}, {Put, "d", []byte("4")},
	}}); !errors.Is(e, ErrCapacity) {
		t.Fatalf("want ErrCapacity, got %v", e)
	}
	if n := len(s.Snapshot().Records); n != 0 {
		t.Fatal("records not rolled back", n)
	}
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("1")},
	}}); !errors.Is(e, ErrCapacity) {
		t.Fatalf("want ErrCapacity (total bytes), got %v", e)
	}
}

func TestRevisionAndGenerationRollback(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	if x.Generation != 1 || x.Revision != 1 {
		t.Fatal(x)
	}
	b := s.Snapshot()
	// Failing batch allocates revisions internally; must not leak.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "zz", nil}}})
	if got := s.Snapshot(); !reflect.DeepEqual(b, got) {
		t.Fatal("state leaked after rollback", got)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if x.Generation != 2 || x.Revision != 2 {
		t.Fatal("revision/generation not contiguous after rollback", x)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 {
		t.Fatal(e, x)
	}
	if g := s.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestDeleteMissingAndGet(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", e)
	}
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "1" {
		t.Fatal("snapshot aliases internal state")
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
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 {
		t.Fatal("expected empty store", len(snap.Records))
	}
	if snap.Generation != 32*20*2 {
		t.Fatal("generation mismatch", snap.Generation)
	}
}

func TestConcurrentRevisionUniqueness(t *testing.T) {
	s, _ := New(Options{MaxRecords: 256, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4096})
	const n = 16
	revs := make([]uint64, n)
	var w sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			x, e := s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("r%02d", i), []byte("v")}}})
			if e != nil {
				t.Error(e)
				return
			}
			revs[i] = x.Revision
		}()
	}
	w.Wait()
	seen := map[uint64]bool{}
	for _, r := range revs {
		if r == 0 || seen[r] {
			t.Fatal("revisions not unique/contiguous", revs)
		}
		seen[r] = true
	}
	if s.Snapshot().NextRevision != n+1 {
		t.Fatal(s.Snapshot().NextRevision)
	}
}
