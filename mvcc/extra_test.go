package mvcc

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestPutCapacityAndOwnership(t *testing.T) {
	s := mustStore(t, 4)
	if _, err := s.Put("ab", []byte("cd")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("x", nil); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	if _, err := s.Put("ab", []byte("efgh")); !errors.Is(err, ErrCapacity) {
		t.Fatalf("replace overflow: %v", err)
	}
	// Replacement within budget succeeds; failed puts changed nothing.
	if _, err := s.Put("ab", []byte("e")); err != nil {
		t.Fatal(err)
	}
	kvs, _ := s.Range("ab", "", 0)
	if string(kvs[0].Value) != "e" {
		t.Fatalf("kvs=%+v", kvs)
	}
	// Mutating the input after Put must not affect the store.
	in := []byte("zz")
	if _, err := s.Put("k", in); err == nil {
		in[0] = 'Q'
		got, _ := s.Range("k", "", 0)
		if string(got[0].Value) != "zz" {
			t.Fatalf("input aliased: %q", got[0].Value)
		}
	}
}

func TestDeleteEventTombstone(t *testing.T) {
	s := mustStore(t, 64)
	s.Put("a", []byte("1"))
	s.Put("a", []byte("2"))
	e, ok, err := s.Delete("a")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if e.KV.Value != nil || e.KV.CreateRevision != 1 || e.KV.ModRevision != 3 || e.KV.Version != 2 {
		t.Fatalf("tombstone=%+v", e.KV)
	}
	if e.Prev == nil || string(e.Prev.Value) != "2" {
		t.Fatalf("prev=%+v", e.Prev)
	}
	if kvs, _ := s.Range("a", "", 0); len(kvs) != 0 {
		t.Fatalf("kvs=%+v", kvs)
	}
	if kvs, _ := s.Range("a", "", 2); len(kvs) != 1 {
		t.Fatalf("history=%+v", kvs)
	}
}

func TestRangeRevisionBoundaries(t *testing.T) {
	s := mustStore(t, 64)
	s.Put("a", []byte("1"))
	s.Put("b", []byte("2"))
	if err := s.Compact(1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Range("a", "", 1); !errors.Is(err, ErrCompacted) {
		t.Fatalf("at compact: %v", err)
	}
	if _, err := s.Range("a", "", 0); err != nil {
		t.Fatalf("current after compact: %v", err)
	}
	if _, err := s.Watch("", 0, 0); !errors.Is(err, ErrCompacted) {
		t.Fatalf("watch below compact: %v", err)
	}
	if _, err := s.Watch("", 1, 0); err != nil {
		t.Fatalf("watch at compact: %v", err)
	}
	if _, err := s.Watch("", 3, 0); !errors.Is(err, ErrFutureRevision) {
		t.Fatalf("watch future: %v", err)
	}
	if err := s.Compact(0); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("compact zero: %v", err)
	}
	if err := s.Compact(1); err != nil {
		t.Fatalf("repeat compact: %v", err)
	}
}

func TestCompactionRetainsBaseVersion(t *testing.T) {
	s := mustStore(t, 128)
	s.Put("a", []byte("1")) // rev 1
	s.Put("a", []byte("2")) // rev 2
	s.Put("b", []byte("x")) // rev 3
	if err := s.Compact(2); err != nil {
		t.Fatal(err)
	}
	// Base version of "a" (rev 2) retained: current and later reads work.
	kvs, err := s.Range("a", "", 0)
	if err != nil || len(kvs) != 1 || string(kvs[0].Value) != "2" {
		t.Fatalf("kvs=%+v err=%v", kvs, err)
	}
	if _, err := s.Range("a", "", 2); !errors.Is(err, ErrCompacted) {
		t.Fatalf("compacted read: %v", err)
	}
	// Key fully removed before the compact point disappears from history.
	s.Delete("b") // rev 4
	if err := s.Compact(4); err != nil {
		t.Fatal(err)
	}
	kvs, err = s.Range("b", "", 0)
	if err != nil || len(kvs) != 0 {
		t.Fatalf("kvs=%+v err=%v", kvs, err)
	}
}

func TestTxnValidationAndRollback(t *testing.T) {
	s := mustStore(t, 64)
	s.Put("a", []byte("1"))
	cases := []struct {
		name string
		cmp  []Compare
		succ []Op
		fail []Op
		want error
	}{
		{"bad compare key", []Compare{{Key: "", Target: CompareExists, Result: CompareEqual}}, nil, nil, ErrInvalidKey},
		{"bad compare target", []Compare{{Key: "a", Target: 99, Result: CompareEqual}}, nil, nil, ErrInvalidCompare},
		{"bad compare result", []Compare{{Key: "a", Target: CompareValue, Result: 99}}, nil, nil, ErrInvalidCompare},
		{"exists less", []Compare{{Key: "a", Target: CompareExists, Result: CompareLess}}, nil, nil, ErrInvalidCompare},
		{"bad op type", nil, []Op{{Type: 0, Key: "a"}}, nil, ErrInvalidOp},
		{"empty op key", nil, []Op{{Type: OpPut, Key: ""}}, nil, ErrInvalidKey},
		{"bad op range", nil, []Op{{Type: OpRange, Key: "z", End: "a"}}, nil, ErrInvalidRange},
		{"op revision", nil, []Op{{Type: OpRange, Key: "a", Revision: 1}}, nil, ErrInvalidOp},
		{"put with end", nil, []Op{{Type: OpPut, Key: "a", End: "b"}}, nil, ErrInvalidOp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := s.Snapshot()
			if _, err := s.Txn(tc.cmp, tc.succ, tc.fail); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			after := s.Snapshot()
			if before.Revision != after.Revision || len(before.KVs) != len(after.KVs) {
				t.Fatal("state mutated on validation error")
			}
		})
	}
}

func TestTxnDeleteRecreateSameTxn(t *testing.T) {
	s := mustStore(t, 64)
	s.Put("a", []byte("1"))
	r, err := s.Txn(nil, []Op{
		{Type: OpDelete, Key: "a"},
		{Type: OpPut, Key: "a", Value: []byte("2")},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Events) != 2 || r.Events[0].Type != EventDelete || r.Events[1].Type != EventPut {
		t.Fatalf("events=%+v", r.Events)
	}
	if r.Events[1].KV.CreateRevision != 2 || r.Events[1].KV.Version != 1 {
		t.Fatalf("recreate=%+v", r.Events[1].KV)
	}
	if r.Events[1].Prev != nil {
		t.Fatalf("prev after delete should be nil: %+v", r.Events[1].Prev)
	}
}

func TestTxnDuplicateWritesAndPrev(t *testing.T) {
	s := mustStore(t, 64)
	r, err := s.Txn(nil, []Op{
		{Type: OpPut, Key: "a", Value: []byte("1")},
		{Type: OpPut, Key: "a", Value: []byte("2")},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Events) != 2 || r.Events[0].Sequence != 0 || r.Events[1].Sequence != 1 {
		t.Fatalf("events=%+v", r.Events)
	}
	if r.Events[0].Prev != nil || r.Events[1].Prev == nil || string(r.Events[1].Prev.Value) != "1" {
		t.Fatalf("prevs=%+v", r.Events)
	}
	if r.Events[1].KV.Version != 2 {
		t.Fatalf("kv=%+v", r.Events[1].KV)
	}
	kvs, _ := s.Range("a", "", 0)
	if string(kvs[0].Value) != "2" || kvs[0].Version != 2 {
		t.Fatalf("kvs=%+v", kvs)
	}
}

func TestTxnPutThenDeleteSameKey(t *testing.T) {
	s := mustStore(t, 64)
	r, err := s.Txn(nil, []Op{
		{Type: OpPut, Key: "a", Value: []byte("1")},
		{Type: OpDelete, Key: "a"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Events) != 2 || r.Events[1].Type != EventDelete {
		t.Fatalf("events=%+v", r.Events)
	}
	tomb := r.Events[1].KV
	if tomb.CreateRevision != 1 || tomb.ModRevision != 1 || tomb.Version != 1 || tomb.Value != nil {
		t.Fatalf("tombstone=%+v", tomb)
	}
	if kvs, _ := s.Range("a", "", 0); len(kvs) != 0 {
		t.Fatalf("kvs=%+v", kvs)
	}
	if s.Snapshot().LiveBytes != 0 {
		t.Fatal("live bytes not rolled back to zero")
	}
}

func TestWatchLimitPrefixAndIsolation(t *testing.T) {
	s := mustStore(t, 256)
	s.Put("p/a", []byte("1"))
	s.Put("q/a", []byte("2"))
	s.Put("p/b", []byte("3"))
	events, err := s.Watch("p/", 0, 1)
	if err != nil || len(events) != 1 || events[0].KV.Key != "p/a" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	events, err = s.Watch("p/", 1, 0)
	if err != nil || len(events) != 1 || events[0].KV.Key != "p/b" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	// Mutating returned events must not leak into later reads.
	events[0].KV.Value[0] = 'X'
	if events[0].Prev != nil {
		*events[0].Prev = KV{Key: "evil"}
	}
	again, _ := s.Watch("p/", 1, 0)
	if string(again[0].KV.Value) != "3" {
		t.Fatalf("aliased: %+v", again[0])
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := mustStore(t, 64)
	s.Put("a", []byte("1"))
	snap := s.Snapshot()
	snap.KVs[0].Value[0] = 'X'
	kvs, _ := s.Range("a", "", 0)
	if string(kvs[0].Value) != "1" {
		t.Fatal("snapshot aliases store")
	}
	if snap.CompactRevision != 0 || snap.LiveBytes != 2 {
		t.Fatalf("snap=%+v", snap)
	}
}

func TestConcurrentTxnAndWatch(t *testing.T) {
	s := mustStore(t, 1<<20)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				key := fmt.Sprintf("t/%d/%d", g, i)
				_, err := s.Txn(
					[]Compare{{Key: key, Target: CompareExists, Result: CompareEqual, Revision: 0}},
					[]Op{{Type: OpPut, Key: key, Value: []byte{byte(i)}}},
					nil,
				)
				if err != nil {
					t.Errorf("txn: %v", err)
				}
				if _, err := s.Watch("t/", 0, 0); err != nil {
					t.Errorf("watch: %v", err)
				}
				if _, _, err := s.Delete(fmt.Sprintf("t/%d/%d", g, i-1)); err != nil && i > 0 {
					t.Errorf("delete: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	snap := s.Snapshot()
	if len(snap.KVs) != 4 {
		t.Fatalf("remaining keys=%d", len(snap.KVs))
	}
	if err := s.Compact(snap.Revision); err != nil {
		t.Fatal(err)
	}
	if events, err := s.Watch("", 0, 0); !errors.Is(err, ErrCompacted) || events != nil {
		t.Fatalf("post-compact watch: %v", err)
	}
}
