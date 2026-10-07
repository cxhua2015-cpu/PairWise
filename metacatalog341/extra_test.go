package metacatalog341

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, -1, 1, 1}, {1, 1, -1, 1}, {1, 1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	ok := []string{"a", "abc", "a-1", "_", "0", "z_9"}
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "abcd", "A", "a b", "a.b", "é", "a/b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
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
	// Delete 缺失键本应 ErrNotFound，但结构校验优先。
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// 中途超过 MaxRecords，但批次末回落，应成功。
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("y")}, {Put, "c", []byte("z")}, {Delete, "c", nil}}}); e != nil {
		t.Fatal(e)
	}
	// 中途超过总字节，批次末回落，应成功。
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("zzzz")}, {Put, "a", []byte("q")}}}); e != nil {
		t.Fatal(e)
	}
	// 批次末超限应失败并回滚。
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("zzzz")}, {Put, "b", []byte("w")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Records) != len(b.Records) {
		t.Fatal("no rollback", got, b)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", nil}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestRevisionAndGenerationSemantics(t *testing.T) {
	s := store(t)
	r0 := s.Snapshot()
	if r0.Generation != 0 || r0.NextRevision != 1 {
		t.Fatal(r0)
	}
	// 空批次不改变 generation。
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	// Delete 不分配 revision。
	x, e = s.Apply(Batch{Ops: []Op{{Put, "a", nil}, {Put, "b", nil}, {Delete, "b", nil}}})
	if e != nil || x.Generation != 1 || x.Revision != 2 {
		t.Fatal(e, x)
	}
	if len(x.Changed) != 1 || x.Changed[0].Name != "a" || x.Changed[0].Revision != 1 {
		t.Fatal(x.Changed)
	}
	// 失败批次不推进 revision。
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", nil}, {Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	x, e = s.Apply(Batch{Ops: []Op{{Put, "c", nil}}})
	if e != nil || x.Revision != 3 || x.Generation != 2 {
		t.Fatal(e, x)
	}
	if s.Snapshot().NextRevision != 4 {
		t.Fatal(s.Snapshot())
	}
}

func TestGetErrors(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	// revision 连续分配：NextRevision = 1 + 成功 Put 总数。
	if snap.NextRevision != 1+32*20*2 {
		t.Fatal(snap.NextRevision)
	}
	if snap.Generation != 32*20 {
		t.Fatal(snap.Generation)
	}
}
