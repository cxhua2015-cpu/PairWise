package artifactindex

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, -2, 1, 1}, {1, 1, -3, 1}, {1, 1, 1, -4},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameRules(t *testing.T) {
	s := store(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "aaaaaaaaaaaaa", "a\tb"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", n, e)
		}
	}
	s2, _ := New(Options{MaxRecords: 16, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 128})
	good := []string{"a", "0", "-", "_", "abc-09_xyz", "abcdefghijkl"}
	for _, n := range good {
		if _, e := s2.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
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

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of a missing name would fail with ErrNotFound, but the
	// structurally invalid op later in the batch must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
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

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	// Mid-batch the store holds 4 records / 4 bytes over the record cap,
	// but the deletes bring it back under before commit.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1111")}, {Put, "b", []byte("2222")},
		{Put, "c", []byte("3333")}, {Put, "d", []byte("4444")},
		{Delete, "a", nil}, {Delete, "b", nil},
	}})
	if e != nil || len(x.Changed) != 2 {
		t.Fatal(e, x)
	}
	// Exceeding total value bytes at batch end fails and rolls back.
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "e", []byte("55555555")}, {Put, "f", []byte("6")}}})
	if !errors.Is(e, ErrCapacity) || len(s.Snapshot().Records) != len(b.Records) {
		t.Fatal(e)
	}
}

func TestRevisionContinuityAndGeneration(t *testing.T) {
	s := store(t)
	x1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x1.Generation != 1 || x1.Revision != 2 {
		t.Fatal(x1)
	}
	// Failed batch must not consume revisions or bump generation.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Delete, "zz", nil}}})
	snap := s.Snapshot()
	if snap.Generation != 1 || snap.NextRevision != 3 {
		t.Fatal(snap)
	}
	// Delete allocates no revision; empty batch bumps nothing.
	x2, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if x2.Generation != 2 || x2.Revision != 2 {
		t.Fatal(x2)
	}
	x3, _ := s.Apply(Batch{})
	if x3.Generation != 2 || x3.Revision != 2 || x3.Changed != nil {
		t.Fatal(x3)
	}
	x4, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if x4.Revision != 3 || x4.Changed[0].Revision != 3 {
		t.Fatal(x4)
	}
}

func TestChangedDedupAndOrder(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")}, {Put, "a", []byte("2")},
		{Put, "b", []byte("3")}, {Delete, "a", nil}, {Put, "a", []byte("4")},
	}})
	if e != nil || len(x.Changed) != 2 {
		t.Fatal(e, x)
	}
	if x.Changed[0].Name != "a" || x.Changed[1].Name != "b" {
		t.Fatal(x.Changed)
	}
	if x.Changed[0].Revision != 4 || x.Changed[1].Revision != 3 {
		t.Fatal(x.Changed)
	}
	if string(x.Changed[0].Value) != "4" || string(x.Changed[1].Value) != "3" {
		t.Fatal(x.Changed)
	}
	// Put followed by Delete of the same name leaves no change entry.
	x2, _ := s.Apply(Batch{Ops: []Op{{Put, "tmp", []byte("1")}, {Delete, "tmp", nil}}})
	if len(x2.Changed) != 0 {
		t.Fatal(x2.Changed)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'z'
	snap.Records[0].Name = "zz"
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "2" {
		t.Fatal(r)
	}
}

func TestGetMissing(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 || snap.Generation != 32*20 {
		t.Fatal(len(snap.Records), snap.Generation)
	}
	// Revisions are contiguous: 3 puts per batch, all committed.
	if snap.NextRevision != 32*20*2+1 {
		t.Fatal(snap.NextRevision)
	}
}
