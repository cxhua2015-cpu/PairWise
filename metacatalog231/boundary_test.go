package metacatalog231

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, 1, 1, -5},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	ok := []string{"a", "z0-_", "abcd", "9", "-", "_"}
	for _, n := range ok {
		if err := s.ValidateBatch(Batch{Ops: []Op{{Put, n, []byte("v")}}}); err != nil {
			t.Fatalf("name %q should be valid: %v", n, err)
		}
	}
	bad := []string{"", "abcde", "A", "a b", "a.b", "é", "a/b", strings.Repeat("x", 5)}
	for _, n := range bad {
		if err := s.ValidateBatch(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q should be invalid: %v", n, err)
		}
		if _, _, err := s.Get(n); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("get %q should be invalid: %v", n, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a", Value: []byte("v")}}},
		{Ops: []Op{{Kind: 99, Name: "a", Value: []byte("v")}}},
		{Ops: []Op{{Put, "a", nil}}},
		{Ops: []Op{{Put, "a", []byte{}}}},
		{Ops: []Op{{Put, "a", []byte("12345")}}},
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
	if err := s.ValidateBatch(Batch{Ops: []Op{{Put, "a", []byte("1234")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestValidationPrecedesStateRead(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	// Structurally invalid op before a Delete of a missing record must yield
	// ErrInvalidInput, not ErrNotFound.
	_, err := s.Apply(Batch{Ops: []Op{{Put, "bad!", []byte("v")}, {Delete, "missing", nil}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 {
		t.Fatal(snap)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Mid-batch the catalog holds 2 records / 4 bytes; at the end only 1/2.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("xx")},
		{Put, "b", []byte("yy")},
		{Delete, "a", nil},
	}})
	if err != nil || len(r.Changed) != 2 {
		t.Fatal(r, err)
	}
	st := s.Stats()
	if st.Records != 1 || st.TotalValueBytes != 2 || st.Generation != 1 || st.NextRevision != 3 {
		t.Fatalf("%+v", st)
	}
}

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xx")}}}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	_, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("yy")}, {Put, "c", []byte("zz")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	_, err = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("yyyyy")}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	_, err = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("zzzz")}, {Put, "b", []byte("w")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestDeleteMissingRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	_, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Delete, "a", nil}, {Delete, "ghost", nil}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after not-found failure")
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'z'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "2" {
		t.Fatal("snapshot aliases store")
	}
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}}})
	r.Changed[0].Value[0] = 'z'
	rec, _, _ = s.Get("d")
	if string(rec.Value) != "4" {
		t.Fatal("result aliases store")
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Snapshot(), c.Snapshot()) {
		t.Fatal("clone diverges")
	}
	if s.Stats() != c.Stats() {
		t.Fatal("clone clocks diverge")
	}
	// Mutating the clone must not affect the original and vice versa.
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3")}}})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("9")}}})
	if _, ok, _ := s.Get("c"); ok {
		t.Fatal("clone write leaked into original")
	}
	rec, ok, _ := c.Get("b")
	if !ok || string(rec.Value) != "2" {
		t.Fatal("original write leaked into clone")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
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
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, []byte("v")}}})
			}
		}()
	}
	wg.Wait()
	st := s.Stats()
	snap := s.Snapshot()
	if st.Records != 16 || snap.Generation != st.Generation || snap.NextRevision != st.NextRevision {
		t.Fatalf("stats %+v snapshot %+v", st, snap)
	}
}

func TestConcurrentCloneConsistency(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("k%d", i)
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				}
			}
		}()
	}
	for i := 0; i < 50; i++ {
		c, err := s.Clone()
		if err != nil {
			t.Fatal(err)
		}
		st := c.Stats()
		snap := c.Snapshot()
		if st.Records != len(snap.Records) || st.Generation != snap.Generation || st.NextRevision != snap.NextRevision {
			t.Fatalf("inconsistent clone: %+v vs %+v", st, snap)
		}
	}
	close(stop)
	wg.Wait()
}
