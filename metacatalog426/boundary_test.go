package metacatalog426

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
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
}

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	ok := Batch{Ops: []Op{{Put, "a_0", []byte("v")}}}
	if _, err := s.Apply(ok); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "abcd", "A", "a b", "a/b", "é"} {
		if _, err := s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, err)
		}
		if _, _, err := s.Get(name); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("get %q: %v", name, err)
		}
	}
}

func TestValueAndTotalCapacity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 3})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xxx")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xx")}, {Put, "b", []byte("xx")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(s.Snapshot().Records); n != 0 {
		t.Fatal("capacity failure leaked records:", n)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("xx")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestMaxRecordsCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Put, "b", []byte("v")}, {Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if n := len(s.Snapshot().Records); n != 1 {
		t.Fatal(n)
	}
}

func TestDeleteMissingAndUnknownKind(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("nope"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestRevisionContinuityAcrossBatches(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	r2, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3")}}})
	if r1.Revision != 2 || r2.Revision != 3 || r2.Generation != 2 {
		t.Fatal(r1, r2)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 4 || snap.Records[0].Name != "b" || snap.Records[1].Name != "c" {
		t.Fatal(snap)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "v" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneClocksAndIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Snapshot(), s.Snapshot()) {
		t.Fatal("clone diverged")
	}
	r, _ := c.Apply(Batch{Ops: []Op{{Put, "b", []byte("w")}}})
	if r.Revision != 2 || s.Snapshot().NextRevision != 2 {
		t.Fatal("clone shares clock with original")
	}
}

func TestPreviewErrorParity(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	cases := []Batch{
		{Ops: []Op{{Put, "bad!", []byte("x")}}},
		{Ops: []Op{{Delete, "missing", nil}}},
		{Ops: []Op{{Put, "b", []byte("12345678")}, {Put, "c", []byte("12345678")}, {Put, "d", []byte("12345678")}}},
	}
	for _, b := range cases {
		_, _, _, perr := s.Preview(b)
		c, _ := s.Clone()
		_, aerr := c.Apply(b)
		if !errors.Is(perr, aerr) {
			t.Fatalf("preview=%v apply=%v", perr, aerr)
		}
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("k%02d", i)
			_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			_, _, _, _ = s.Preview(Batch{Ops: []Op{{Delete, k, nil}}})
			_, _, _ = s.Get(k)
			_ = s.Snapshot()
			_ = s.Stats()
			_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("w")}}})
			c, _ := s.Clone()
			_, _ = c.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	wg.Wait()
	st := s.Stats()
	if st.Records != 32 || st.NextRevision != 33 {
		t.Fatal(st)
	}
}
