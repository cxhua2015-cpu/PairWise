package featureflags

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

func TestStructuralValidationBeforeState(t *testing.T) {
	s := store(t)
	// Unknown kind and extra Delete payload are rejected even when a
	// later op would fail a state check (Delete of missing name).
	for _, b := range []Batch{
		{Ops: []Op{{Kind(0), "a", nil}, {Delete, "missing", nil}}},
		{Ops: []Op{{Kind(99), "a", nil}, {Delete, "missing", nil}}},
		{Ops: []Op{{Delete, "a", []byte("x")}, {Delete, "missing", nil}}},
		{Ops: []Op{{Put, "a", make([]byte, 9)}, {Delete, "missing", nil}}},
	} {
		if _, e := s.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("got %v", e)
		}
	}
	if g := s.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestNameCharset(t *testing.T) {
	s := store(t)
	for _, n := range []string{"", "Bad", "a b", "a.b", "a/b", "\xe4\xb8\xad", "toolongname123"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: got %v", n, e)
		}
	}
	for _, n := range []string{"a", "0", "-", "_", "a-b_c-9", "abcdefghijkl"} {
		fresh := store(t)
		if _, e := fresh.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	r, e = s.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestRevisionNotAllocatedOnFailure(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	b := s.Snapshot()
	// Fails at end-of-batch capacity: revision must not be consumed.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("12345678")}, {Put, "c", []byte("12345678")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed")
	}
	r, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("w")}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
}

func TestDeleteMissingAndReput(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "a", []byte("2")}}})
	if len(x.Changed) != 1 || x.Changed[0].Revision != 2 || string(x.Changed[0].Value) != "2" {
		t.Fatal(x)
	}
	if n := len(s.Snapshot().Records); n != 1 {
		t.Fatal(n)
	}
}

func TestChangedExcludesDeleted(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("3")}}})
	if e != nil || len(x.Changed) != 1 || x.Changed[0].Name != "b" {
		t.Fatal(x, e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	if snap.NextRevision != 4 || snap.Generation != 1 {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestTotalValueCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 10, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Mid-batch the total exceeds the cap, but the final state fits.
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}, {Put, "b", []byte("1234")}, {Delete, "a", nil}}})
	if e != nil || x.Revision != 2 {
		t.Fatal(x, e)
	}
	// Overwrite accounting: replacing a value must not double-count.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("abcd")}}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
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
				r, ok, e := s.Get(k)
				if e == nil && ok && string(r.Value) != "v" {
					t.Error("bad value")
				}
				prev := s.Snapshot()
				if prev.Generation > 0 && prev.NextRevision < 2 {
					t.Error("bad snapshot")
				}
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 16 {
		t.Fatal(len(snap.Records))
	}
	// Revisions are contiguous: 2 puts per iteration * 50 * 32 goroutines.
	if want := uint64(2 * 50 * 32); snap.NextRevision != want+1 {
		t.Fatalf("NextRevision=%d want %d", snap.NextRevision, want+1)
	}
}
