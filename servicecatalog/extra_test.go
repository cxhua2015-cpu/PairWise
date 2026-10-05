package servicecatalog

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

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
}

func TestNameBoundaries(t *testing.T) {
	s := store(t) // MaxNameBytes 12
	ok := "a0-_" + "zzzzzzzz" // 12 bytes
	if _, e := s.Apply(Batch{Ops: []Op{{Put, ok, []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{"", "A", "a b", "a/b", "名字", "toolongname123"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, bad, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", bad, e)
		}
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// 即使 Delete 目标不存在，结构错误也必须优先返回 ErrInvalidInput。
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestFinalCapacityOnly(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	// 中途超过记录数上限，但批次末回落，应成功。
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")}, {Put, "b", []byte("2")},
		{Put, "c", []byte("3")}, {Put, "d", []byte("4")}, {Delete, "d", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// 中途总字节超限，批次末回落，应成功。
	_, e = s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Delete, "a", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// 批次末超限，应失败并回滚。
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestRevisionContinuityAndGeneration(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	r2, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3")}}})
	if r1.Generation != 1 || r1.Revision != 2 || r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r1, r2)
	}
	snap := s.Snapshot()
	if snap.Generation != 2 || snap.NextRevision != 4 {
		t.Fatal(snap)
	}
	// 失败批次不得消耗 revision 或 generation。
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "x", []byte("1")}, {Delete, "nope", nil}}})
	if s.Snapshot().NextRevision != 4 || s.Snapshot().Generation != 2 {
		t.Fatal("failed batch leaked revision/generation")
	}
}

func TestChangedContents(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Delete, "b", nil}, {Put, "c", []byte("3")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// b 被删除，Changed 只含 a、c，按名称排序。
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[1].Name != "c" {
		t.Fatal(r.Changed)
	}
	r.Changed[0].Value[0] = 'z'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "2" {
		t.Fatal("Changed aliases internal state")
	}
}

func TestGetErrors(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'z'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("svc-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 {
		t.Fatal(len(snap.Records))
	}
	// 每轮两个非空成功批次（Put、Delete 批次中 Delete 非空也算），generation 应等于成功非空批次数。
	if snap.Generation != 32*20*2 {
		t.Fatal(snap.Generation)
	}
	if snap.NextRevision != 32*20+1 {
		t.Fatal(snap.NextRevision)
	}
}
