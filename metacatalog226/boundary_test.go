package metacatalog226

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

func TestStructuralBoundaries(t *testing.T) {
	s, err := New(Options{MaxRecords: 4, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a", Value: []byte("v")}}},                // unknown kind
		{Ops: []Op{{Kind: 99, Name: "a", Value: []byte("v")}}},               // unknown kind
		{Ops: []Op{{Put, "", []byte("v")}}},                                  // empty name
		{Ops: []Op{{Put, "abcd", []byte("v")}}},                              // name too long
		{Ops: []Op{{Put, "A", []byte("v")}}},                                 // uppercase
		{Ops: []Op{{Put, "a b", []byte("v")}}},                               // space
		{Ops: []Op{{Put, "a.b", []byte("v")}}},                               // punctuation
		{Ops: []Op{{Put, "a", nil}}},                                         // nil put value
		{Ops: []Op{{Put, "a", []byte("toolong")}}},                           // value too long
		{Ops: []Op{{Delete, "a", []byte{}}}},                                 // delete with value
		{Ops: []Op{{Put, "ok-1_", []byte("v")}, {Put, "bad!", []byte("v")}}}, // late failure
	}
	for i, b := range cases {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d validate: %v", i, err)
		}
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d apply: %v", i, err)
		}
	}
	if got := s.Stats(); got.Records != 0 || got.Generation != 0 || got.NextRevision != 1 {
		t.Fatalf("state mutated by invalid batches: %+v", got)
	}
	// Boundary-valid names and values are accepted.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a-_", []byte("12")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := mustStore(t)
	r1, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.Apply(Batch{})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Generation != r1.Generation || len(r2.Changed) != 0 {
		t.Fatalf("empty batch changed generation: %+v -> %+v", r1, r2)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, err := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	// Temporarily exceeds both limits mid-batch, legal at the end.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("aaaa")},
		{Put, "b", []byte("bbbb")},
		{Put, "c", []byte("cccc")},
		{Delete, "a", nil},
		{Delete, "b", nil},
		{Put, "c", []byte("dd")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Revision != 4 {
		t.Fatalf("revision: %d", r.Revision)
	}
	st := s.Stats()
	if st.Records != 1 || st.TotalValueBytes != 2 {
		t.Fatalf("stats: %+v", st)
	}
	// Exceeding at the end fails and rolls back.
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "x", []byte("eeee")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	if after := s.Snapshot(); after.Generation != before.Generation || after.NextRevision != before.NextRevision {
		t.Fatal("capacity failure did not roll back clocks")
	}
}

func TestTotalValueBytesCapacity(t *testing.T) {
	s, err := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("ab")}, {Put, "b", []byte("cd")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("total value capacity: %v", err)
	}
	if n := len(s.Snapshot().Records); n != 0 {
		t.Fatalf("rollback left %d records", n)
	}
}

func TestDeleteMissingAndReput(t *testing.T) {
	s := mustStore(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Delete, "a", nil}, {Delete, "a", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete: %v", err)
	}
	// Put then Delete in one batch leaves no record but allocates a revision.
	r, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("v")}, {Delete, "b", nil}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get("b"); ok {
		t.Fatal("b should be deleted")
	}
	if r.Revision != 1 || s.Snapshot().NextRevision != 2 {
		t.Fatalf("revision accounting: %+v", r)
	}
}

func TestGetInvalidNameAndMissing(t *testing.T) {
	s := mustStore(t)
	if _, _, err := s.Get("bad!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("get invalid: %v", err)
	}
	if _, ok, err := s.Get("nope"); err != nil || ok {
		t.Fatalf("get missing: %v %v", ok, err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := mustStore(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, ok, err := s.Get("a")
	if err != nil || !ok || string(r.Value) != "xy" {
		t.Fatalf("snapshot aliases store: %q", r.Value)
	}
	res, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("zz")}}})
	if err != nil {
		t.Fatal(err)
	}
	res.Changed[0].Value[0] = 'Q'
	r, _, _ = s.Get("c")
	if string(r.Value) != "zz" {
		t.Fatalf("result aliases store: %q", r.Value)
	}
}

func TestCloneClocksAndIsolation(t *testing.T) {
	s := mustStore(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	ss, cs := s.Stats(), c.Stats()
	if ss != cs {
		t.Fatalf("clocks differ: %+v vs %+v", ss, cs)
	}
	// Mutating the clone's snapshot data must not touch the original.
	snap := c.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, _, _ := s.Get("a")
	if string(r.Value) != "v" {
		t.Fatal("clone aliases original value")
	}
	// Both stores continue to allocate revisions independently.
	if _, err := c.Apply(Batch{Ops: []Op{{Put, "b", []byte("w")}}}); err != nil {
		t.Fatal(err)
	}
	r2, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("w")}}})
	if err != nil || r2.Revision != 2 {
		t.Fatalf("original clock affected by clone: %+v %v", r2, err)
	}
}

func mustStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestConcurrentMixed(t *testing.T) {
	s, err := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				if err := s.ValidateBatch(Batch{Ops: []Op{{Put, name, []byte("v")}}}); err != nil {
					t.Error(err)
				}
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, name, nil}}})
		}()
	}
	wg.Wait()
	st := s.Stats()
	if st.Records != 0 || st.TotalValueBytes != 0 {
		t.Fatalf("leaked state: %+v", st)
	}
	// 32 goroutines x 21 successful non-empty batches.
	if st.Generation != 32*21 {
		t.Fatalf("generation: %d", st.Generation)
	}
	// Only the 20 put batches per goroutine allocate a revision; deletes do not.
	if st.NextRevision != 32*20+1 {
		t.Fatalf("next revision: %d", st.NextRevision)
	}
}

func TestConcurrentCloneAndSnapshot(t *testing.T) {
	s, err := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	if err != nil {
		t.Fatal(err)
	}
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
				_ = c.Snapshot()
				_ = s.Stats()
			}
		}()
	}
	for i := 0; i < 200; i++ {
		_, _ = s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("n%d", i%16), []byte("v")}}})
	}
	close(stop)
	wg.Wait()
}
