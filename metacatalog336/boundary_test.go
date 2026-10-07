package metacatalog336

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
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameCharsetAndLength(t *testing.T) {
	s, _ := New(Options{MaxRecords: 16, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	bad := []string{"", "A", "a b", "a.b", "a/b", "中文", "abcdefghijklm"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	good := []string{"a", "0", "-", "_", "a-b_c-9", "abcdefghijkl"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValueTooLarge(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// 即使 Delete 的目标不存在，结构校验错误也必须优先返回。
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("empty batch changed mutated state")
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if x.Generation != 1 {
		t.Fatal(x.Generation)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if x.Generation != 2 {
		t.Fatal(x.Generation)
	}
}

func TestRevisionContinuousNoDeleteAlloc(t *testing.T) {
	s, _ := New(Options{MaxRecords: 10, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x.Revision != 2 {
		t.Fatal(x.Revision)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if x.Revision != 2 {
		t.Fatalf("delete allocated revision: %d", x.Revision)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if x.Revision != 3 {
		t.Fatal(x.Revision)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}}})
	b := s.Snapshot()
	// 末态 4 条记录 > MaxRecords=3，且总字节 24 > 16。
	_, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "d", []byte("2")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("capacity failure not rolled back")
	}
}

func TestTotalValueBytesBoundary(t *testing.T) {
	s := store(t)
	// a=8 + b=8 = 16，恰好达标。
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// 覆盖写只计末态：a 覆写为 8 字节后总量仍为 16。
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("87654321")}}}); e != nil {
		t.Fatal(e)
	}
	// a=8 + b=8 + c=1 = 17 > 16。
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestDeleteMissingAndDoubleDelete(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Delete, "a", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, ok, _ := s.Get("a"); !ok {
		t.Fatal("rollback lost record a")
	}
}

func TestGetInvalidName(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); ok || e != nil {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "1" {
		t.Fatal("snapshot aliases internal state")
	}
	if snap.Records[0].Name != "a" || snap.NextRevision != 2 || snap.Generation != 1 {
		t.Fatal(snap)
	}
}

func TestChangedSortedAndDeepCopied(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Delete, "c", nil}, {Put, "b", []byte("3")}}})
	names := []string{x.Changed[0].Name, x.Changed[1].Name, x.Changed[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
	}
	x.Changed[1].Value[0] = 'z'
	r, _, _ := s.Get("b")
	if string(r.Value) != "3" {
		t.Fatal("result aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 12, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte(j)}}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 || snap.Generation != 32*20 || snap.NextRevision != 32*20+1 {
		t.Fatal(len(snap.Records), snap.Generation, snap.NextRevision)
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}

func TestConcurrentContendedKeys(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, "x", []byte("v")}, {Put, "y", []byte("v")}, {Delete, "y", nil}}})
			}
		}()
	}
	w.Wait()
	if _, ok, _ := s.Get("x"); !ok {
		t.Fatal("x missing")
	}
	if _, ok, _ := s.Get("y"); ok {
		t.Fatal("y should be deleted")
	}
}
