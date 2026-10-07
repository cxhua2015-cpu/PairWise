package metacatalog311

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
	for _, o := range []Options{
		{MaxRecords: 0, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 0, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 0, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 0},
		{MaxRecords: -1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestNameCharset(t *testing.T) {
	s := store(t)
	good := []string{"a", "abc-123_x", "0"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "this-name-is-too-long"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	for _, k := range []Kind{0, 3, 255} {
		if _, e := s.Apply(Batch{Ops: []Op{{Kind: k, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: %v", k, e)
		}
	}
}

func TestValueTooLarge(t *testing.T) {
	s := store(t) // MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("empty batch changed state")
	}
}

func TestValidationBeforeState(t *testing.T) {
	s := store(t)
	// Delete of a missing name would fail with ErrNotFound, but a later
	// structurally invalid op must win because validation runs first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	// Record-count overflow at batch end.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}, {Put, "d", []byte("y")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	// Total value bytes overflow at batch end.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("12345678")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestDeleteSemantics(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}}})
	if e != nil || r.Revision != 1 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be gone")
	}
	// Delete does not allocate a revision.
	r, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "c", []byte("1")}, {Delete, "b", nil}, {Put, "d", []byte("1")}}})
	if e != nil || r.Revision != 4 {
		t.Fatal(e, r)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "c" || r.Changed[1].Name != "d" {
		t.Fatal(r.Changed)
	}
}

func TestGenerationAndSnapshotIsolation(t *testing.T) {
	s := store(t)
	for i := 1; i <= 3; i++ {
		r, e := s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("k%d", i), []byte("v")}}})
		if e != nil || r.Generation != uint64(i) {
			t.Fatal(e, r)
		}
	}
	snap := s.Snapshot()
	if snap.Generation != 3 || snap.NextRevision != 4 || len(snap.Records) != 3 {
		t.Fatal(snap)
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get(snap.Records[0].Name)
	if string(r.Value) != "v" {
		t.Fatal("snapshot aliases internal state")
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
			k := fmt.Sprintf("k%02d", i%16)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}}})
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if snap.Generation == 0 || len(snap.Records) != 16 {
		t.Fatal(snap.Generation, len(snap.Records))
	}
}
