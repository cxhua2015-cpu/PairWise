package metacatalog291

import (
	"errors"
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
		{Ops: []Op{{Put, "", []byte("v")}}},                              // empty name
		{Ops: []Op{{Put, "UPPER", []byte("v")}}},                         // uppercase
		{Ops: []Op{{Put, "has space", []byte("v")}}},                     // space
		{Ops: []Op{{Put, "abcdefghijklm", []byte("v")}}},                 // name too long
		{Ops: []Op{{Put, "a", []byte("123456789")}}},                     // value too long
		{Ops: []Op{{Put, "a", nil}}},                                     // empty put value
		{Ops: []Op{{Delete, "a", []byte("x")}}},                          // delete with value
		{Ops: []Op{{Kind(99), "a", nil}}},                                // unknown kind
		{Ops: []Op{{Put, "ok", []byte("v")}, {Delete, "missing?", nil}}}, // second op invalid
	}
	for i, b := range cases {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d validate: %v", i, err)
		}
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d apply: %v", i, err)
		}
	}
	if got := s.Snapshot(); got.Generation != 0 || got.NextRevision != 1 || len(got.Records) != 0 {
		t.Fatalf("state mutated: %+v", got)
	}
	// Boundary-valid names must pass.
	ok := Batch{Ops: []Op{{Put, "a-z_0-9", []byte("v")}}}
	if err := s.ValidateBatch(ok); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsClocks(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatalf("empty batch after put: %+v %v", r, err)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")},
	}}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	// Total value bytes exceeded only at batch end.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	// Record count exceeded only at batch end.
	if _, err := s.Apply(Batch{Ops: []Op{
		{Delete, "a", nil}, {Put, "c", []byte("1")}, {Put, "d", []byte("2")}, {Put, "e", []byte("3")},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision {
		t.Fatalf("clocks moved: %+v -> %+v", before, got)
	}
	// Mid-batch overflow that resolves by the end must succeed.
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("12345678")}, {Delete, "a", nil},
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteMissingAndReput(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Delete, "a", nil}, {Put, "a", []byte("w")}}})
	if err != nil || r.Revision != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	rec, ok, err := s.Get("a")
	if err != nil || !ok || string(rec.Value) != "w" || rec.Revision != 2 {
		t.Fatalf("%+v %v %v", rec, ok, err)
	}
	if _, ok, _ = s.Get("missing"); ok {
		t.Fatal("unexpected hit")
	}
	if _, _, err = s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")},
	}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatalf("order: %+v", snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "2" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneIndependence(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got.Generation != 1 || got.NextRevision != 2 || got.Records != 1 {
		t.Fatalf("clone clocks: %+v", got)
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if s.Stats().Records != 1 || c.Stats().Records != 0 {
		t.Fatal("clone shares state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, err := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for j := 0; j < 20; j++ {
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
	if st.Records != 16 || st.NextRevision != 16*20+1 {
		t.Fatalf("%+v", st)
	}
	snap := s.Snapshot()
	if snap.Generation != st.Generation || snap.NextRevision != st.NextRevision {
		t.Fatal("inconsistent linearizable views")
	}
}

func TestConcurrentCloneAndApply(t *testing.T) {
	s, err := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a' + i))
			for j := 0; j < 10; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				c, err := s.Clone()
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := c.Apply(Batch{Ops: []Op{{Delete, name, nil}}}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if s.Stats().Records != 8 {
		t.Fatal(s.Stats())
	}
}
