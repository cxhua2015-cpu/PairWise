package readyqueue325

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func mustQueue(t *testing.T, o Options) *Queue {
	t.Helper()
	q, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {4, 0}, {-1, 8}, {4, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestInvalidIDs(t *testing.T) {
	q := mustQueue(t, Options{MaxItems: 8, MaxIDBytes: 4})
	for _, id := range []string{"", "A", "a b", "abcde", "a.b", "中文"} {
		_, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: id}}})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	for _, id := range []string{"a", "abcd", "a-1_", "z9"} {
		if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: id}}}); err != nil {
			t.Fatalf("id %q: %v", id, err)
		}
	}
}

func TestUnknownKindAndNegativeValues(t *testing.T) {
	q := mustQueue(t, Options{MaxItems: 8, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Kind(99), ID: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: -1, Ops: []Op{{Kind: Enqueue, ID: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Pop(0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Pop(0, -2); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if got := q.Snapshot(); got.Generation != 0 || got.NextRevision != 1 || len(got.Items) != 0 {
		t.Fatalf("state changed after invalid batches: %+v", got)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := mustQueue(t, Options{MaxItems: 8, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Kind: Enqueue, ID: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Kind: Enqueue, ID: "b"}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	// 失败批次不推进时间，但成功 Pop 推进。
	if _, err := q.Pop(6, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Kind: Enqueue, ID: "b"}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if got := q.Snapshot(); got.Now != 6 {
		t.Fatalf("now=%d", got.Now)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	q := mustQueue(t, Options{MaxItems: 8, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	// 同批次内重复入队同样视为已存在。
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "b"}, {Kind: Enqueue, ID: "b"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Cancel, ID: "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// 同批次先取消再入队合法。
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Cancel, ID: "a"}, {Kind: Enqueue, ID: "a", Priority: 9}}}); err != nil {
		t.Fatal(err)
	}
	if got := q.Snapshot(); len(got.Items) != 1 || got.Items[0].Priority != 9 {
		t.Fatalf("%+v", got)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := mustQueue(t, Options{MaxItems: 8, MaxIDBytes: 8})
	r1, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a"}, {Kind: Enqueue, ID: "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatalf("%+v", r1)
	}
	// 失败批次不消耗 revision、不增加 generation。
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Cancel, ID: "zzz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r2, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "c"}}})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatalf("%+v", r2)
	}
	s := q.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatalf("%+v", s)
	}
	revs := map[string]uint64{}
	for _, it := range s.Items {
		revs[it.ID] = it.Revision
	}
	if !reflect.DeepEqual(revs, map[string]uint64{"a": 1, "b": 2, "c": 3}) {
		t.Fatalf("%v", revs)
	}
}

func TestEmptyBatchNoop(t *testing.T) {
	q := mustQueue(t, Options{MaxItems: 8, MaxIDBytes: 8})
	before := q.Snapshot()
	r, err := q.Apply(Batch{Now: 10})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 0 || r.Revision != 0 {
		t.Fatalf("%+v", r)
	}
	if got := q.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("empty batch changed state: %+v -> %+v", before, got)
	}
}

func TestCapacityCheckedAtEnd(t *testing.T) {
	q := mustQueue(t, Options{MaxItems: 2, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a"}, {Kind: Enqueue, ID: "b"}}}); err != nil {
		t.Fatal(err)
	}
	// 净数量不变但中途超过上限：只在末尾检查，应成功。
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "c"}, {Kind: Cancel, ID: "a"}}}); err != nil {
		t.Fatal(err)
	}
	// 末尾超出上限：整体回滚。
	before := q.Snapshot()
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "d"}, {Kind: Enqueue, ID: "e"}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := q.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("capacity failure not rolled back: %+v", got)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := mustQueue(t, Options{MaxItems: 16, MaxIDBytes: 8})
	_, err := q.Apply(Batch{Ops: []Op{
		{Kind: Enqueue, ID: "p1a", Priority: 1, ReadyAt: 0},
		{Kind: Enqueue, ID: "p2b", Priority: 2, ReadyAt: 5},
		{Kind: Enqueue, ID: "p2a", Priority: 2, ReadyAt: 5},
		{Kind: Enqueue, ID: "p2c", Priority: 2, ReadyAt: 3},
		{Kind: Enqueue, ID: "future", Priority: 9, ReadyAt: 100},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Pop(10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "p2c" || got[1].ID != "p2a" {
		t.Fatalf("%+v", got)
	}
	got, err = q.Pop(10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "p2b" || got[1].ID != "p1a" {
		t.Fatalf("%+v", got)
	}
	// future 尚未就绪。
	got, err = q.Pop(10, 1)
	if err != nil || len(got) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = q.Pop(100, 1)
	if err != nil || len(got) != 1 || got[0].ID != "future" {
		t.Fatalf("%+v %v", got, err)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := mustQueue(t, Options{MaxItems: 8, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a", Priority: 1}}}); err != nil {
		t.Fatal(err)
	}
	s := q.Snapshot()
	s.Items[0].Priority = 999
	s.Items = append(s.Items, Item{ID: "evil"})
	if again := q.Snapshot(); again.Items[0].Priority != 1 || len(again.Items) != 1 {
		t.Fatalf("internal state mutated via snapshot: %+v", again)
	}
	got, err := q.Pop(0, 1)
	if err != nil || len(got) != 1 {
		t.Fatal(err, got)
	}
	got[0].ReadyAt = 12345
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q := mustQueue(t, Options{MaxItems: 256, MaxIDBytes: 16})
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("w%d-i%d", w, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Kind: Enqueue, ID: id, Priority: i}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Kind: Cancel, ID: id}}})
			}
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatalf("duplicate id %q", it.ID)
		}
		seen[it.ID] = true
	}
	for i := 1; i < len(s.Items); i++ {
		if less(s.Items[i], s.Items[i-1]) {
			t.Fatalf("snapshot not in canonical order at %d", i)
		}
	}
}

func TestConcurrentUniqueRevisions(t *testing.T) {
	q := mustQueue(t, Options{MaxItems: 512, MaxIDBytes: 16})
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				_, _ = q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: fmt.Sprintf("w%d-i%d", w, i)}}})
			}
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	if len(s.Items) != 320 {
		t.Fatal(len(s.Items))
	}
	revs := map[uint64]bool{}
	for _, it := range s.Items {
		if revs[it.Revision] {
			t.Fatalf("duplicate revision %d", it.Revision)
		}
		revs[it.Revision] = true
	}
	if s.NextRevision != 321 {
		t.Fatal(s.NextRevision)
	}
}
