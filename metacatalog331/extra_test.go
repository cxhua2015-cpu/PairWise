package metacatalog331

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

func TestNameRules(t *testing.T) {
	s := store(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "this-name-is-too-long"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefghijkl"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	for _, k := range []Kind{0, 3, 255} {
		if _, e := s.Apply(Batch{Ops: []Op{{k, "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: %v", k, e)
		}
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
	// 未知 kind 与不存在键的 Delete 同批：结构校验优先，必须报 ErrInvalidInput。
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}, {Kind(9), "a", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	g0 := s.Snapshot().Generation
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != g0 || s.Snapshot().Generation != g0 {
		t.Fatal(e, r)
	}
	r, e = s.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != g0 {
		t.Fatal(e, r)
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if e != nil || r.Generation != 1 || s.Snapshot().Generation != 1 {
		t.Fatal(e, r)
	}
}

func TestRevisionContiguousAndDeleteSkips(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if r1.Revision != 2 {
		t.Fatal(r1.Revision)
	}
	r2, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3")}}})
	if r2.Revision != 3 || s.Snapshot().NextRevision != 4 {
		t.Fatal(r2, s.Snapshot().NextRevision)
	}
	rec, ok, _ := s.Get("c")
	if !ok || rec.Revision != 3 {
		t.Fatal(rec, ok)
	}
}

func TestDeleteNotFoundRollbackRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	snap := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "nope", nil}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(snap, s.Snapshot()) {
		t.Fatal(e)
	}
	// revision 未消耗：下一次 Put 仍取 2。
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if r.Revision != 2 {
		t.Fatal(r.Revision)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// 中途超限但批末回落到限制内：成功。
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Put, "c", []byte("33")},
		{Delete, "a", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// 批末总字节超限：整体回滚。
	snap := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2222")}, {Put, "c", []byte("3333")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(snap, s.Snapshot()) {
		t.Fatal(e)
	}
	// 批末记录数超限：整体回滚。
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(snap, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestChangedDeduplicatedAndSorted(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}, {Delete, "a", nil},
	}})
	if e != nil || len(r.Changed) != 2 {
		t.Fatal(e, r)
	}
	if r.Changed[0].Name != "a" || r.Changed[1].Name != "b" {
		t.Fatal(r.Changed)
	}
	if r.Changed[1].Revision != 3 || string(r.Changed[1].Value) != "3" {
		t.Fatal(r.Changed[1])
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, _, _ := s.Get("a")
	if string(r.Value) != "xy" {
		t.Fatal(string(r.Value))
	}
	// 快照间互不影响。
	s1 := s.Snapshot()
	s2 := s.Snapshot()
	s1.Records[0].Value[0] = 'Q'
	if !reflect.DeepEqual(s2.Records[0].Value, []byte("xy")) {
		t.Fatal(s2.Records[0].Value)
	}
}

func TestGetInvalidName(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); e != nil || ok {
		t.Fatal(ok, e)
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
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
	// 单调性：generation 与 revision 只增不减。
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "final", []byte("x")}}})
	if r.Generation <= snap.Generation || r.Revision < snap.NextRevision {
		t.Fatal(r, snap)
	}
}

func TestConcurrentFailedBatchesRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "base", []byte("1")}}})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%d", i)
			for j := 0; j < 10; j++ {
				// 这批必然失败（Delete 不存在键），不得留下任何痕迹。
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, "zz-none", nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 1 || snap.Records[0].Name != "base" || snap.Generation != 1 || snap.NextRevision != 2 {
		t.Fatal(snap)
	}
}
