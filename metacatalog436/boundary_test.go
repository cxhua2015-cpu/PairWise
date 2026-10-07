package metacatalog436

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, opts := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, err := New(opts); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts %+v: got %v", opts, err)
		}
	}
}

func TestStructuralBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	cases := []struct {
		name  string
		batch Batch
		err   error
	}{
		{"empty name", Batch{Ops: []Op{{Put, "", nil}}}, ErrInvalidInput},
		{"too long name", Batch{Ops: []Op{{Put, "abcd", nil}}}, ErrInvalidInput},
		{"uppercase", Batch{Ops: []Op{{Put, "Abc", nil}}}, ErrInvalidInput},
		{"space", Batch{Ops: []Op{{Put, "a b", nil}}}, ErrInvalidInput},
		{"non-ascii", Batch{Ops: []Op{{Put, "é", nil}}}, ErrInvalidInput},
		{"unknown kind", Batch{Ops: []Op{{Kind(99), "a", nil}}}, ErrInvalidInput},
		{"zero kind", Batch{Ops: []Op{{Kind(0), "a", nil}}}, ErrInvalidInput},
		{"delete with value", Batch{Ops: []Op{{Delete, "a", []byte("x")}}}, ErrInvalidInput},
		{"oversize value", Batch{Ops: []Op{{Put, "a", []byte("xyz")}}}, ErrInvalidInput},
		{"max name ok", Batch{Ops: []Op{{Put, "a_0", nil}}}, nil},
		{"max value ok", Batch{Ops: []Op{{Put, "a", []byte("xy")}}}, nil},
		{"nil value ok", Batch{Ops: []Op{{Put, "a", nil}}}, nil},
	}
	for _, tc := range cases {
		if err := s.ValidateBatch(tc.batch); !errors.Is(err, tc.err) {
			t.Fatalf("%s: ValidateBatch got %v want %v", tc.name, err, tc.err)
		}
		before := s.Snapshot()
		if _, err := s.Apply(tc.batch); !errors.Is(err, tc.err) {
			t.Fatalf("%s: Apply got %v want %v", tc.name, err, tc.err)
		}
		if tc.err != nil && !reflect.DeepEqual(before, s.Snapshot()) {
			t.Fatalf("%s: failed batch mutated state", tc.name)
		}
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Transiently exceeds MaxRecords but ends within limits: must succeed.
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("aa")}, {Put, "b", []byte("bb")}, {Put, "c", nil}, {Delete, "c", nil},
	}}); err != nil {
		t.Fatal(err)
	}
	// Total value bytes exceeded only at the end.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("aaa")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("want ErrCapacity, got %v", err)
	}
	// Record count exceeded at the end; state must roll back.
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "c", nil}, {Put, "d", nil}}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("want ErrCapacity, got %v", err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("capacity failure did not roll back")
	}
}

func TestEmptyBatchKeepsClocks(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	res, err := s.Apply(Batch{})
	if err != nil || !reflect.DeepEqual(res, Result{}) {
		t.Fatal(res, err)
	}
	snap := s.Snapshot()
	if snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatalf("empty batch changed clocks: %+v", snap)
	}
}

func TestGetValidationAndIsolation(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if _, _, err := s.Get("BAD!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("nope"); ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	rec, ok, _ := s.Get("a")
	if !ok {
		t.Fatal("missing record")
	}
	rec.Value[0] = 'X'
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Y'
	rec2, _, _ := s.Get("a")
	if string(rec2.Value) != "v" {
		t.Fatal("returned slices alias internal state")
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Snapshot(), c.Snapshot()) || s.Stats() != c.Stats() {
		t.Fatal("clone diverged from source")
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", nil}}}); err != nil {
		t.Fatal(err)
	}
	if got := len(c.Snapshot().Records); got != 1 {
		t.Fatalf("source mutation leaked into clone: %d", got)
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if got := len(s.Snapshot().Records); got != 2 {
		t.Fatalf("clone mutation leaked into source: %d", got)
	}
}

func TestPreviewErrorParity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	batches := []Batch{
		{Ops: []Op{{Put, "bad!", nil}}},
		{Ops: []Op{{Delete, "ghost", nil}}},
		{Ops: []Op{{Put, "a", nil}, {Put, "b", nil}}},
	}
	for _, b := range batches {
		c, _ := s.Clone()
		_, wantErr := c.Apply(b)
		res, snap, stats, err := s.Preview(b)
		if !errors.Is(err, wantErr) {
			t.Fatalf("preview err %v, apply err %v", err, wantErr)
		}
		if err != nil && (!reflect.DeepEqual(res, Result{}) || !reflect.DeepEqual(snap, Snapshot{}) || !reflect.DeepEqual(stats, Stats{})) {
			t.Fatal("failed preview returned non-zero values")
		}
	}
	if got := s.Stats(); got.Records != 0 || got.Generation != 0 || got.NextRevision != 1 {
		t.Fatalf("preview mutated receiver: %+v", got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 25; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, nil}}})
				_, _, _, _ = s.Preview(Batch{Ops: []Op{{Put, name, []byte("w")}}})
				if j%5 == 0 {
					_, _ = s.Clone()
				}
			}
		}()
	}
	wg.Wait()
	stats := s.Stats()
	snap := s.Snapshot()
	if stats.Records != 16 || len(snap.Records) != 16 {
		t.Fatalf("stats=%+v records=%d", stats, len(snap.Records))
	}
	if stats.Generation != 16*25 || snap.Generation != stats.Generation {
		t.Fatalf("generation mismatch: %+v vs %+v", stats, snap)
	}
	if stats.NextRevision != 16*25+1 || snap.NextRevision != stats.NextRevision {
		t.Fatalf("revision mismatch: %+v vs %+v", stats, snap)
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted by name")
		}
	}
}
