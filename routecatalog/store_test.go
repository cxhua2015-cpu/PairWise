package routecatalog

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
		{-1, 1, 1, 1}, {1, 1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameAndValueBoundaries(t *testing.T) {
	s := store(t)
	// 名称长度恰好等于上限应成功。
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abcdefghijkl", []byte("x")}}}); e != nil {
		t.Fatal(e)
	}
	// 超长名称、空名称、大写、非法字符、未知 kind 均为 ErrInvalidInput。
	for _, op := range []Op{
		{Put, "abcdefghijklm", []byte("x")},
		{Put, "", []byte("x")},
		{Put, "ABC", []byte("x")},
		{Put, "a b", []byte("x")},
		{Put, "a.b", []byte("x")},
		{Kind(0), "a", nil},
		{Kind(99), "a", nil},
		{Put, "ok", make([]byte, 9)}, // MaxValueBytes=8
	} {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	// 合法字符全集：小写字母、数字、连字符、下划线。
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a-z_09", []byte("x")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// 第一个 op 非法时，即使后续 op 会触发 NotFound，也必须返回 ErrInvalidInput。
	_, e := s.Apply(Batch{Ops: []Op{{Put, "bad!", []byte("x")}, {Delete, "missing", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// 结构校验覆盖整个批次：末尾非法同样整体失败。
	_, e = s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
}

func TestGenerationBumpsOncePerBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	r, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r, e)
	}
}

func TestDeleteNoRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	r, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r.Revision != 1 {
		t.Fatal(r, e)
	}
	if snap := s.Snapshot(); snap.NextRevision != 2 {
		t.Fatal(snap)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s := store(t) // MaxRecords=3, MaxTotalValueBytes=16
	// 中间状态超限（4 条记录）但最终回落到 3 条，应成功。
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")}, {Put, "b", []byte("2")},
		{Put, "c", []byte("3")}, {Put, "d", []byte("4")},
		{Delete, "d", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// 最终状态超限则 ErrCapacity 且整体回滚。
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("12345678")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	// 总字节超限：3 条记录但 24 字节 > 16。
	_, e = s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("12345678")},
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestOverwriteAccounting(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 9, MaxTotalValueBytes: 13})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345")}, {Put, "b", []byte("12345")}}}); e != nil {
		t.Fatal(e)
	}
	// 覆盖不增加总字节，仍应在容量内。
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("123456789")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestGetNotFoundAndInvalid(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
	snap := s.Snapshot()
	if len(snap.Records) != 0 {
		t.Fatal(len(snap.Records))
	}
	// revision 单调：每个 Put 恰好分配一个。
	if snap.NextRevision != 32*20+1 {
		t.Fatal(snap.NextRevision)
	}
}

func TestConcurrentApplyAtomicity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%d", i%4)
			// 半数的批次因容量失败；状态必须始终一致可读。
			_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("vvvv")}, {Put, "zz", []byte("vvvv")}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	total := 0
	for _, r := range snap.Records {
		total += len(r.Value)
	}
	if len(snap.Records) > 4 || total > 16 {
		t.Fatal("capacity invariant violated")
	}
}
