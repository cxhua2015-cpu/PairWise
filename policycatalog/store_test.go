package policycatalog

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func opts() Options {
	return Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64}
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, e := New(Options{o.MaxRecords, o.MaxNameBytes, o.MaxValueBytes, o.MaxTotalValueBytes}); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s, _ := New(opts())
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 0 {
		t.Fatal(snap)
	}
}

func TestStructuralValidationEdges(t *testing.T) {
	s, _ := New(opts())
	bad := []Op{
		{Put, "", []byte("v")},
		{Put, "Upper", []byte("v")},
		{Put, "has space", []byte("v")},
		{Put, "toolongname", []byte("v")},
		{Put, "ok", make([]byte, 9)},
		{Kind(0), "ok", nil},
		{Kind(99), "ok", nil},
	}
	for _, op := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(op, e)
		}
	}
	for _, n := range []string{"a-b_c", "z9", "12345678"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatal(n, e)
		}
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s, _ := New(opts())
	// Second op is structurally invalid; first op would have failed with
	// ErrNotFound if state were read first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Transiently exceeds record and byte caps, fine at batch end.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12")},
		{Put, "b", []byte("34")},
		{Put, "c", []byte("56")},
		{Delete, "a", nil},
		{Delete, "c", nil},
	}})
	if e != nil || len(s.Snapshot().Records) != 1 {
		t.Fatal(r, e)
	}
	// Final state exceeds record cap.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "x", nil}, {Put, "y", nil}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Final state exceeds total value bytes.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("12345")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != 1 || len(got.Records) != 1 {
		t.Fatal(got)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	s, _ := New(opts())
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	// Delete allocates no revision.
	r2, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if r2.Generation != 2 || r2.Revision != 2 {
		t.Fatal(r2)
	}
	// Failed batch rolls back generation and revision.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", nil}, {Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.Generation != 2 || snap.NextRevision != 2 || len(snap.Records) != 1 {
		t.Fatal(snap)
	}
}

func TestChangedDedupSorted(t *testing.T) {
	s, _ := New(opts())
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")},
		{Put, "a", []byte("2")},
		{Put, "b", []byte("3")},
		{Put, "c", []byte("4")},
		{Delete, "c", nil},
	}})
	if e != nil || len(r.Changed) != 2 {
		t.Fatal(r, e)
	}
	if r.Changed[0].Name != "a" || r.Changed[1].Name != "b" || string(r.Changed[1].Value) != "3" || r.Changed[1].Revision != 3 {
		t.Fatal(r.Changed)
	}
}

func TestGetMissingAndInvalid(t *testing.T) {
	s, _ := New(opts())
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("Bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s, _ := New(opts())
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'q'
	r, _, _ := s.Get("a")
	if string(r.Value) != "x" {
		t.Fatal(r)
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
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte(j)}}}})
				r, ok, _ := s.Get(k)
				if ok {
					_ = r.Value[0]
				}
				snap := s.Snapshot()
				for _, rec := range snap.Records {
					_ = rec.Value
				}
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	if n := len(s.Snapshot().Records); n != 0 {
		t.Fatal(n)
	}
}
