package metacatalog391

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
}

func TestNameCharset(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	ok := []string{"a", "abc-123_def", "0", "-", "_"}
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "中文", "a/b", "aaaaaaaaaaaaa"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndValueLimit(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete 不存在的键，但批次含非法名：应先报结构错误。
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(e, x)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if x.Generation != 1 {
		t.Fatal(x)
	}
	x, _ = s.Apply(Batch{Ops: nil})
	if x.Generation != 1 || s.Snapshot().Generation != 1 {
		t.Fatal(x)
	}
}

func TestDeleteAbsentAndReput(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "a", []byte("2")}}})
	if e != nil || len(x.Changed) != 1 || string(x.Changed[0].Value) != "2" || x.Changed[0].Revision != 2 {
		t.Fatal(e, x)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("3")}, {Delete, "a", nil}}})
	if len(x.Changed) != 0 {
		t.Fatal(x.Changed)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be gone")
	}
}

func TestRevisionConsecutiveOnPutOnly(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if x.Revision != 2 || s.Snapshot().NextRevision != 3 {
		t.Fatal(x)
	}
	// 失败批次不消耗 revision。
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}, {Delete, "ghost", nil}}})
	if s.Snapshot().NextRevision != 3 {
		t.Fatal(s.Snapshot())
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// 中途超过记录数，但批次末回到限制内：成功。
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "a", nil}}}); e != nil {
		t.Fatal(e)
	}
	// 批次末总字节超限：整体回滚。
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "b", nil}, {Put, "d", []byte("3333")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	// 批次末记录数超限。
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}}})
	snap := s.Snapshot()
	names := []string{snap.Records[0].Name, snap.Records[1].Name, snap.Records[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
	}
	snap.Records[0].Value[0] = 'z'
	snap.Records[0].Name = "zzz"
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal(r)
	}
}

func TestGetInvalidName(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("absent"); e != nil || ok {
		t.Fatal(e, ok)
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
			k := fmt.Sprintf("k-%02d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Put, k, []byte("w")}}})
				if i%3 == 0 {
					_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
				}
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}

func TestConcurrentGenerationMonotonic(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	var w sync.WaitGroup
	const n = 16
	for i := 0; i < n; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			_, _ = s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("k%d", i%4), []byte("v")}}})
		}()
	}
	w.Wait()
	if s.Snapshot().Generation != n {
		t.Fatal(s.Snapshot().Generation)
	}
}
