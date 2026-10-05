package resourcecatalog161

import (
	"errors"
	"fmt"
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

func TestNameCharsetAndLength(t *testing.T) {
	s := store(t)
	for _, name := range []string{"", "A", "a b", "a.b", "a/b", "é", "abcdefghijklm"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, e)
		}
		if _, _, e := s.Get(name); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", name, e)
		}
	}
	for _, name := range []string{"a", "0", "-", "_", "a-b_c9", "abcdefghijkl"} {
		fresh := store(t)
		if _, e := fresh.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", name, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
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
	if e != nil || x.Generation != 0 {
		t.Fatal(e, x)
	}
	x, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if e != nil || x.Generation != 1 || x.Revision != 1 {
		t.Fatal(e, x)
	}
	if _, e = s.Apply(Batch{Ops: []Op{}}); e != nil || s.Snapshot().Generation != 1 {
		t.Fatal(e)
	}
}

func TestRevisionNotAllocatedOnDeleteOrFailure(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || x.Revision != 1 {
		t.Fatal(e, x)
	}
	if s.Snapshot().NextRevision != 2 {
		t.Fatal(s.Snapshot())
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "missing", nil}}})
	if s.Snapshot().NextRevision != 2 {
		t.Fatal("failed batch must not consume revisions")
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}}})
	// Intermediate state has 2 records / 6 bytes, final state fits.
	x, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("34")}, {Delete, "a", nil}}})
	if e != nil || len(x.Changed) != 1 || x.Changed[0].Name != "b" {
		t.Fatal(e, x)
	}
	// Final state exceeds total bytes -> rollback.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("123")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if _, ok, _ := s.Get("c"); ok {
		t.Fatal("rollback failed")
	}
	// Final state exceeds record count -> rollback.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "d", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestChangedSortedAndDeduped(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "a", nil}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(x.Changed) != 1 || x.Changed[0].Name != "c" || x.Changed[0].Revision != 3 || string(x.Changed[0].Value) != "3" {
		t.Fatal(x.Changed)
	}
}

func TestSnapshotSortedAndDeepCopy(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestGetNotFound(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 16 {
		t.Fatal(len(snap.Records))
	}
	seen := map[uint64]bool{}
	for _, r := range snap.Records {
		if seen[r.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[r.Revision] = true
		if r.Revision >= snap.NextRevision {
			t.Fatal("revision out of range")
		}
	}
}
