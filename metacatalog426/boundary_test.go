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

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "abc", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "abcd", "A", "a b", "a.b", "é"} {
		if _, err := s.Apply(Batch{Ops: []Op{{Put, bad, []byte("v")}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", bad, err)
		}
		if _, _, err := s.Get(bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("get %q: %v", bad, err)
		}
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}, {Kind: 99, Name: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 5)}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestEmptyBatchAndCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 6})
	r, err := s.Apply(Batch{})
	if err != nil || !reflect.DeepEqual(r, Result{}) || s.Snapshot().Generation != 0 {
		t.Fatal(r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("123")}}}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	// Total value bytes would become 8 > 6; must roll back revision too.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1234")}, {Put, "c", []byte("1")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("capacity failure leaked state or revision")
	}
	// Record-count capacity.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "c", []byte("1")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("record capacity failure leaked state")
	}
}

func TestDeleteThenPutSameBatch(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	r, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("zz")}}})
	if err != nil || r.Generation != 2 || r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r, err)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be deleted")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%d", i%8)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_, _, _, _ = s.Preview(Batch{Ops: []Op{{Put, k, []byte("w")}}})
				_ = s.ValidateBatch(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	st := s.Stats()
	if st.Records != len(snap.Records) || st.Generation != snap.Generation || st.NextRevision != snap.NextRevision {
		t.Fatal("stats/snapshot disagree")
	}
	c, err := s.Clone()
	if err != nil || !reflect.DeepEqual(c.Snapshot(), snap) {
		t.Fatal("clone diverged")
	}
}
