package metacatalog261

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
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s := store(t)
	ok := strings.Repeat("a", 12)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, ok, []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", strings.Repeat("a", 13), "A", "a b", "a/b", "é"} {
		if _, err := s.Apply(Batch{Ops: []Op{{Put, bad, []byte("v")}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", bad, err)
		}
		if _, _, err := s.Get(bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("get %q: %v", bad, err)
		}
	}
	if _, ok, err := s.Get("missing"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestValueBoundaries(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(err, ErrInvalidInput) {
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

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Transiently exceeds record capacity but ends within it.
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "a", nil},
	}}); err != nil {
		t.Fatal(err)
	}
	// Transiently exceeds total value bytes via overwrite, ends within.
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1234")}, {Put, "b", []byte("5")}, {Delete, "c", nil},
	}}); err != nil {
		t.Fatal(err)
	}
	// Final state exceeds total value bytes: rollback.
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1234")}, {Put, "c", []byte("5")},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("capacity failure must roll back")
	}
	// Final state exceeds record capacity: rollback.
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")}, {Put, "d", []byte("2")},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestGenerationIncrementsOncePerBatch(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if err != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, err)
	}
	if g := s.Stats().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestChangedSortedAndDeduplicated(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "a", nil},
	}})
	if err != nil || len(r.Changed) != 2 {
		t.Fatal(r, err)
	}
	if r.Changed[0].Name != "a" || r.Changed[0].Value != nil || r.Changed[1].Name != "c" || r.Changed[1].Revision != 3 {
		t.Fatal(r.Changed)
	}
}

func TestStatsLinearizable(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}, {Put, "b", []byte("z")}}}); err != nil {
		t.Fatal(err)
	}
	st := s.Stats()
	if st.Records != 2 || st.TotalValueBytes != 3 || st.Generation != 1 || st.NextRevision != 3 {
		t.Fatalf("%+v", st)
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != s.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	r, _, _ := c.Get("a")
	r.Value[0] = 'X'
	r2, _, _ := s.Get("a")
	if string(r2.Value) != "v" {
		t.Fatal("clone aliases original value")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if s.Stats().Records != 1 || c.Stats().Records != 0 {
		t.Fatal("clone diverged unexpectedly")
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
			name := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte{byte(j)}}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, []byte("v")}}})
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, name, nil}}})
		}()
	}
	wg.Wait()
	if n := s.Stats().Records; n != 0 {
		t.Fatal(n)
	}
	if s.Stats().Generation != 32*21 {
		t.Fatal(s.Stats().Generation)
	}
}

func TestConcurrentCloneAndSnapshotIsolation(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 512})
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				c, err := s.Clone()
				if err != nil {
					t.Error(err)
					return
				}
				snap := c.Snapshot()
				for _, rec := range snap.Records {
					rec.Value[0] ^= 0xff
				}
			}
		}()
	}
	for i := 0; i < 200; i++ {
		_, _ = s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("n%d", i%10), []byte("v")}}})
	}
	close(stop)
	wg.Wait()
}
