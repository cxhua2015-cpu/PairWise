package metacatalog251

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

func TestStructuralBoundaries(t *testing.T) {
	s := store(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},                    // unknown kind
		{Ops: []Op{{Kind: 99, Name: "a"}}},                   // unknown kind
		{Ops: []Op{{Put, "", []byte("v")}}},                  // empty name
		{Ops: []Op{{Put, "Upper", []byte("v")}}},             // uppercase
		{Ops: []Op{{Put, "a b", []byte("v")}}},               // space
		{Ops: []Op{{Put, "abcdefghijkl3", []byte("v")}}},     // name too long (13 > 12)
		{Ops: []Op{{Put, "a", make([]byte, 9)}}},             // value too long (9 > 8)
		{Ops: []Op{{Delete, "a", []byte{0}}}},                // delete with payload
		{Ops: []Op{{Put, "ok", []byte("v")}, {Put, "?", []byte("v")}}}, // bad op later in batch
	}
	for i, b := range cases {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	// Boundary-accepting names and values.
	ok := Batch{Ops: []Op{
		{Put, "a-z_09", []byte("12345678")},
		{Put, "abcdefghijkl", nil},
	}}
	if _, err := s.Apply(ok); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state exceeds both limits, final state fits.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
		{Delete, "b", nil},
	}})
	if err != nil || len(r.Changed) != 1 || r.Changed[0].Name != "c" {
		t.Fatal(r, err)
	}
	// Final state exceeds record capacity: full rollback.
	before := s.Snapshot()
	_, err = s.Apply(Batch{Ops: []Op{{Put, "x", []byte("1")}, {Put, "y", []byte("1")}}})
	if !errors.Is(err, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(err)
	}
	// Final state exceeds total value bytes.
	_, err = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("4444")}, {Put, "d", []byte("1")}}})
	if !errors.Is(err, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(err)
	}
}

func TestRevisionContinuityAndDeleteRollback(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if r1.Revision != 2 {
		t.Fatal(r1)
	}
	// Failed batch must not consume revisions.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Delete, "missing", nil}}})
	r2, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if r2.Revision != 3 || r2.Generation != 2 {
		t.Fatal(r2)
	}
	// Delete allocates no revision.
	r3, _ := s.Apply(Batch{Ops: []Op{{Delete, "c", nil}}})
	if r3.Revision != 3 || r3.Generation != 3 || len(r3.Changed) != 0 {
		t.Fatal(r3)
	}
	if _, ok, _ := s.Get("c"); ok {
		t.Fatal("c should be deleted")
	}
}

func TestGetInvalidNameAndMissing(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("Bad!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("nope"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'x'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Snapshot(), s.Snapshot()) {
		t.Fatal("clone diverges")
	}
	// Mutating the clone must not affect the original; clocks continue independently.
	r, _ := c.Apply(Batch{Ops: []Op{{Put, "b", []byte("w")}}})
	if r.Revision != 2 {
		t.Fatal(r)
	}
	if len(s.Snapshot().Records) != 1 || s.Stats().Generation != 1 {
		t.Fatal("clone write leaked into original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	st := s.Stats()
	if st.Records != 0 || st.TotalValueBytes != 0 || st.Generation != 32*21 {
		t.Fatalf("%+v", st)
	}
	// Revisions are consecutive and never reused: 640 puts.
	if st.NextRevision != 32*20+1 {
		t.Fatalf("nextRevision=%d", st.NextRevision)
	}
}

func TestConcurrentCloneConsistency(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 512})
	var w sync.WaitGroup
	stop := make(chan struct{})
	w.Add(1)
	go func() {
		defer w.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("k%d", i%8), []byte("v")}}})
		}
	}()
	for i := 0; i < 50; i++ {
		c, err := s.Clone()
		if err != nil {
			t.Fatal(err)
		}
		snap := c.Snapshot()
		total := 0
		for _, r := range snap.Records {
			total += len(r.Value)
		}
		if st := c.Stats(); st.Records != len(snap.Records) || st.TotalValueBytes != total || st.Generation != snap.Generation {
			t.Fatalf("inconsistent clone: %+v vs %+v", st, snap)
		}
	}
	close(stop)
	w.Wait()
}
