package readyqueue220

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func mustQueue(t *testing.T, maxItems, maxIDBytes int) *Queue {
	t.Helper()
	q, err := New(Options{MaxItems: maxItems, MaxIDBytes: maxIDBytes})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}, {0, 0}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := mustQueue(t, 8, 4)
	bad := []string{"", "abcde", "A", "a b", "a.b", "é", "a/b"}
	for _, id := range bad {
		_, err := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	good := []string{"a", "z0-_", "1234", "----"}
	for _, id := range good {
		if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); err != nil {
			t.Fatalf("id %q: %v", id, err)
		}
	}
}

func TestUnknownKindAndStructuralFirst(t *testing.T) {
	q := mustQueue(t, 8, 8)
	// 未知 kind。
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// 结构校验先于状态读取：批次含非法 ID 与重复 Enqueue，应报 ErrInvalidInput 而非 ErrExists。
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	_, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "BAD ID", 0, 0}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := mustQueue(t, 8, 8)
	if _, err := q.Apply(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	// 失败的 Apply 不回滚时间之外也不应推进时间；成功后时间应已推进。
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
	if _, err := q.Pop(5, 1); err != nil {
		t.Fatal(err)
	}
}

func TestExistsAndNotFound(t *testing.T) {
	q := mustQueue(t, 8, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "b", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestCapacityRollback(t *testing.T) {
	q := mustQueue(t, 2, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	before := q.Snapshot()
	// 末尾容量检查：净超出应失败并整体回滚（含 revision）。
	_, err := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	// 同批次先取消再入队，净容量不变应成功。
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := mustQueue(t, 8, 8)
	r0 := q.Snapshot()
	if r0.Generation != 0 || r0.NextRevision != 1 {
		t.Fatal(r0)
	}
	// 空批次：generation 不变。
	if _, err := q.Apply(Batch{}); err != nil {
		t.Fatal(err)
	}
	if g := q.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
	r, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	s := q.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	// 失败批次不消耗 revision 与 generation。
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Cancel, "zz", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if s2 := q.Snapshot(); s2.Generation != 1 || s2.NextRevision != 3 {
		t.Fatal(s2)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := mustQueue(t, 8, 8)
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "late", 9, 100},
		{Enqueue, "b", 1, 2},
		{Enqueue, "a", 1, 2},
		{Enqueue, "c", 1, 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// now=2：late 未就绪；同优先级按 ReadyAt 升序、ID 升序。
	got, err := q.Pop(2, 10)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if !reflect.DeepEqual(ids, []string{"c", "a", "b"}) {
		t.Fatal(ids)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("pop did not remove items")
	}
	// n 限制数量。
	if _, err := q.Pop(100, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	got, err = q.Pop(100, 1)
	if err != nil || len(got) != 1 || got[0].ID != "late" {
		t.Fatal(got, err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := mustQueue(t, 8, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 0}}}); err != nil {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if s.Items[0].ID != "b" || s.Items[1].ID != "a" {
		t.Fatal("snapshot not in canonical order", s.Items)
	}
	s.Items[0].ID = "mutated"
	popped, err := q.Pop(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	popped[0].Priority = 99
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q := mustQueue(t, 128, 16)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-i%d", g, i)
				_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(0, 1)
				_ = q.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	seen := make(map[string]bool)
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
	}
}
