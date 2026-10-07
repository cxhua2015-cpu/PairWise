package metacatalog401

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

func TestStructuralValidation(t *testing.T) {
	s := store(t)
	cases := []Op{
		{Kind: 0, Name: "a"},
		{Kind: 99, Name: "a"},
		{Put, "", []byte("v")},
		{Put, "Bad", []byte("v")},
		{Put, "a b", []byte("v")},
		{Put, "a/b", []byte("v")},
		{Put, "这个键太长", []byte("v")},
		{Put, "abcdefghijklm", []byte("v")}, // 13 > MaxNameBytes 12
		{Put, "a", []byte("123456789")},     // 9 > MaxValueBytes 8
		{Put, "a", nil},
		{Delete, "a", []byte{}},
	}
	for _, op := range cases {
		if err := s.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	good := []Op{
		{Put, "a-1_b", []byte("")},
		{Put, "abcdefghijkl", []byte("12345678")},
		{Delete, "missing", nil},
	}
	if err := s.ValidateBatch(Batch{Ops: good}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBatchSideEffectFree(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	before := s.Snapshot()
	for _, b := range []Batch{
		{Ops: []Op{{Delete, "a", nil}}},
		{Ops: []Op{{Put, "b", []byte("y")}}},
		{Ops: []Op{{Put, "bad!", []byte("y")}}},
		{},
	} {
		if err := s.ValidateBatch(b); err != nil && !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("ValidateBatch mutated state")
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}}})
	before := s.Snapshot()
	// Exceeds MaxRecords only at batch end.
	_, err := s.Apply(Batch{Ops: []Op{{Put, "c", nil}, {Delete, "a", nil}}})
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("v")}, {Put, "d", []byte("v")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	// Exceeds total value bytes only at batch end.
	_, err = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("1")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state not rolled back")
	}
}

func TestGenerationSemantics(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(r, err)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("y")}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}}})
	if s.Snapshot().Generation != 1 {
		t.Fatal("failed batch bumped generation")
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
}

func TestDeleteMissingAndReput(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "k", []byte("1")}, {Delete, "k", nil}, {Put, "k", []byte("2")}}})
	r, ok, _ := s.Get("k")
	if !ok || string(r.Value) != "2" || r.Revision != 2 {
		t.Fatal(r, ok)
	}
}

func TestGetInvalidName(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("Bad!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("nope"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "x" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneIndependence(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Snapshot(), s.Snapshot()) {
		t.Fatal("clone diverged")
	}
	if got := c.Stats(); got.Generation != 1 || got.NextRevision != 2 || got.Records != 1 || got.TotalValueBytes != 2 {
		t.Fatalf("%+v", got)
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Put, "b", []byte("z")}}})
	if len(s.Snapshot().Records) != 1 {
		t.Fatal("clone write leaked to original")
	}
	rec, _, _ := c.Get("a")
	rec.Value[0] = 'q'
	r2, _, _ := s.Get("a")
	if string(r2.Value) != "xy" {
		t.Fatal("clone aliases original value")
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
			_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
			_, _, _ = s.Get(k)
			_ = s.Snapshot()
			_ = s.Stats()
			_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("x")}}})
			c, err := s.Clone()
			if err == nil {
				_, _ = c.Apply(Batch{Ops: []Op{{Put, "clone-only", []byte("c")}}})
			}
		}()
	}
	w.Wait()
	st := s.Stats()
	if st.Records != 32 || st.Generation != 32 || st.NextRevision != 65 {
		t.Fatalf("%+v", st)
	}
}
