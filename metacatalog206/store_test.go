package metacatalog206

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, -2, 1, 1}, {1, 1, -3, 1}, {1, 1, 1, -4},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestNameValidation(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	bad := []string{"", "A", "a b", "a.b", "中文", "abcde", "a/b", "+x"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", n, e)
		}
	}
	good := []string{"a", "z09-", "abcd", "0", "_", "-"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndValueTooLong(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("toolong")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Delete ignores Value length limits? Keep structural check on name only;
	// ensure a Delete with oversized value still fails structurally per spec on Put only.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	// First op would fail with ErrNotFound, but a later structural error must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
	snap := s.Snapshot()
	if snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if x.Generation != 1 || x.Revision != 2 {
		t.Fatal(x)
	}
	if len(x.Changed) != 1 || x.Changed[0].Name != "b" {
		t.Fatal(x.Changed)
	}
	// Failed batch must not bump generation.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if g := s.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 8})
	// Temporarily exceeds record count mid-batch but ends within limits: OK.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Put, "c", []byte("33")}, {Delete, "a", nil},
	}})
	if e != nil || len(s.Snapshot().Records) != 2 {
		t.Fatal(x, e)
	}
	// Ends over record limit: rollback.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("44")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(s.Snapshot().Records) != 2 {
		t.Fatal(s.Snapshot())
	}
	// Ends over total value bytes: rollback.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("55555")}, {Put, "c", []byte("55555")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	r, _, _ := s.Get("b")
	if string(r.Value) != "22" {
		t.Fatal(r)
	}
}

func TestRevisionContinuityAndRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x.Revision != 2 {
		t.Fatal(x)
	}
	// Failed batch allocates revisions internally; they must roll back.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Delete, "zz", nil}}})
	if n := s.Snapshot().NextRevision; n != 3 {
		t.Fatal(n)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if x.Revision != 3 {
		t.Fatal(x)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	names := []string{snap.Records[0].Name, snap.Records[1].Name, snap.Records[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal(r)
	}
}

func TestGetNotFound(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	r, ok, e := s.Get("nope")
	if e != nil || ok || r.Name != "" {
		t.Fatal(r, ok, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	if n := len(s.Snapshot().Records); n != 0 {
		t.Fatal(n)
	}
}
