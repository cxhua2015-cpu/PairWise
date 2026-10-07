package metacatalog406

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
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidationBoundaries(t *testing.T) {
	s := store(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Put, "", []byte("v")}}},
		{Ops: []Op{{Put, "Upper", []byte("v")}}},
		{Ops: []Op{{Put, "has space", []byte("v")}}},
		{Ops: []Op{{Put, "this-name-is-too-long", []byte("v")}}},
		{Ops: []Op{{Put, "ok", []byte("way-too-long-value")}}},
		{Ops: []Op{{Delete, "a", []byte("x")}}},
		{Ops: []Op{{Delete, "a", []byte{}}}},
	}
	for i, b := range cases {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := s.ValidateBatch(Batch{Ops: []Op{{Put, "a-b_c1", []byte("ok")}}}); err != nil {
		t.Fatal(err)
	}
	if z := s.Stats(); z.Records != 0 || z.Generation != 0 {
		t.Fatalf("validation must be side-effect free: %+v", z)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}}}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	_, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	_, err = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("123456789")}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	_, err = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("12345678")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Records) != 2 {
		t.Fatalf("state changed after failed batch: %+v vs %+v", got, before)
	}
}

func TestDeleteMissingAndRevisionGaps(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if err != nil || r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r, err)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be deleted")
	}
	if z := s.Stats(); z.NextRevision != 3 || z.Records != 1 {
		t.Fatalf("%+v", z)
	}
}

func TestGetInvalidName(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("missing"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != s.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	r, _ := c.Apply(Batch{Ops: []Op{{Put, "a", []byte("zz")}}})
	_ = r
	ra, _, _ := s.Get("a")
	ca, _, _ := c.Get("a")
	if string(ra.Value) != "xy" || string(ca.Value) != "zz" || ca.Revision != 2 {
		t.Fatal("clone not isolated")
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
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("x")}}})
			}
		}()
	}
	w.Wait()
	z := s.Stats()
	if z.Records != 32 || z.Generation != 640 || z.NextRevision != 1+640*2 {
		t.Fatalf("%+v", z)
	}
	if len(s.Snapshot().Records) != 32 {
		t.Fatal("snapshot mismatch")
	}
	c, err := s.Clone()
	if err != nil || c.Stats() != z {
		t.Fatal(err)
	}
}
