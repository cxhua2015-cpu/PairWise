package metacatalog421

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
	if err != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Transiently exceeds both limits, but ends within them.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Put, "c", []byte("3")},
		{Delete, "c", nil}, {Put, "a", []byte("1")}, {Put, "b", []byte("2")},
	}})
	if err != nil || len(r.Changed) != 2 {
		t.Fatal(r, err)
	}
	z := s.Stats()
	if z.Records != 2 || z.TotalValueBytes != 2 || z.Generation != 1 || z.NextRevision != 6 {
		t.Fatalf("%+v", z)
	}
	// Final record count exceeded.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}, {Put, "d", []byte("y")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	// Final total value bytes exceeded.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xxxx")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Stats(); got.Records != 2 || got.Generation != 1 || got.NextRevision != 6 {
		t.Fatalf("rollback broken: %+v", got)
	}
}

func TestRevisionContinuityAcrossFailures(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if err != nil || r.Revision != 1 {
		t.Fatalf("revision leaked from failed batch: %+v %v", r, err)
	}
}

func TestStructuralValidationErrors(t *testing.T) {
	s := store(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Put, "", []byte("x")}}},
		{Ops: []Op{{Put, "Bad", []byte("x")}}},
		{Ops: []Op{{Put, "a b", []byte("x")}}},
		{Ops: []Op{{Put, "way-too-long-name", []byte("x")}}},
		{Ops: []Op{{Put, "a", make([]byte, 9)}}},
		{Ops: []Op{{Delete, "a", []byte{0}}}},
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
	if err := s.ValidateBatch(Batch{Ops: []Op{{Put, "ok-name_1", nil}, {Delete, "ok-name_1", nil}}}); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != 0 || len(got.Records) != 0 {
		t.Fatalf("validation mutated state: %+v", got)
	}
}

func TestGetValidationAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("Bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("missing"); ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, ok, err := s.Get("a")
	if !ok || err != nil || string(r.Value) != "xy" {
		t.Fatalf("snapshot aliases store: %q", r.Value)
	}
	if s.Snapshot().Records[0].Name != "a" {
		t.Fatal("snapshot not sorted/named correctly")
	}
}

func TestSnapshotSorted(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "delta", []byte("1")}, {Put, "alpha", []byte("2")}, {Put, "charlie", []byte("3")},
	}}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range s.Snapshot().Records {
		names = append(names, r.Name)
	}
	if !reflect.DeepEqual(names, []string{"alpha", "charlie", "delta"}) {
		t.Fatal(names)
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Snapshot(), c.Snapshot()) || s.Stats() != c.Stats() {
		t.Fatal("clone diverges")
	}
	// Mutating clone's returned record values must not affect either store.
	r, _, _ := c.Get("a")
	r.Value[0] = 'Q'
	if got, _, _ := s.Get("a"); string(got.Value) != "xy" {
		t.Fatal("clone aliases original value")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if s.Stats().Records != 1 || c.Stats().Records != 0 {
		t.Fatal("clone not independent")
	}
}

func TestPreviewCapacityParity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	batch := Batch{Ops: []Op{{Put, "b", []byte("y")}}}
	if _, _, _, err := s.Preview(batch); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if _, err := s.Apply(batch); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	res, snap, stats, err := s.Preview(Batch{Ops: []Op{{Put, "a", []byte("zz")}}})
	if err != nil || res.Generation != 2 || res.Revision != 2 || stats.TotalValueBytes != 2 || snap.NextRevision != 3 {
		t.Fatal(res, snap, stats, err)
	}
	if got := s.Stats(); got.Generation != 1 || got.NextRevision != 2 || got.TotalValueBytes != 1 {
		t.Fatalf("preview mutated receiver: %+v", got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte{byte(j)}}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				_, _, _, _ = s.Preview(Batch{Ops: []Op{{Put, name, []byte{byte(j + 1)}}}})
				if j%5 == 0 {
					_, _ = s.Clone()
				}
			}
		}()
	}
	wg.Wait()
	z := s.Stats()
	if z.Records != 32 || z.Generation != 640 || z.NextRevision != 641 {
		t.Fatalf("%+v", z)
	}
}
