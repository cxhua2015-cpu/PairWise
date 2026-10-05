package modelcatalog

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	cases := []Options{
		{MaxRecords: 0, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 0, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 0, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 0},
		{MaxRecords: -1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
	}
	for _, o := range cases {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("expected ErrInvalidOptions for %+v, got %v", o, e)
		}
	}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
}

func TestNameValidation(t *testing.T) {
	s := store(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "名字", "this-name-is-too-long"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: expected ErrInvalidInput, got %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: expected ErrInvalidInput, got %v", n, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefghijkl"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndExtraValue(t *testing.T) {
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
	// structural error later in the batch must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValueByteLimits(t *testing.T) {
	s := store(t) // MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestFinalTotalCapacity(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	// Each op fits per-value limits; only the final total exceeds.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")},
		{Put, "b", []byte("12345678")},
		{Put, "c", []byte("1")},
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 0 {
		t.Fatal(n)
	}
	// Delete within the same batch brings the final total back under the cap.
	_, e = s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")},
		{Put, "b", []byte("12345678")},
		{Put, "c", []byte("1")},
		{Delete, "c", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("generation changed on empty batch")
	}
}

func TestRevisionContinuityAndDeleteRollback(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if e != nil || x.Revision != 2 {
		t.Fatal(e, x)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be deleted")
	}
	// Failed batch must not consume revisions.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Delete, "nope", nil}}})
	x, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if e != nil || x.Revision != 3 {
		t.Fatal(e, x)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
	if snap.Generation != 1 || snap.NextRevision != 3 {
		t.Fatal(snap)
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
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	// 32 keys * 20 rounds * 2 puts each = 1280 revisions.
	if snap.NextRevision != 1281 {
		t.Fatal(snap.NextRevision)
	}
	if snap.Generation != 640 {
		t.Fatal(snap.Generation)
	}
}
