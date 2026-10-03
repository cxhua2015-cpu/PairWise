package mvcc

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustStore(t *testing.T, cap int) *Store {
	t.Helper()
	s, err := New(Options{MaxLiveBytes: cap})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestConstructionAndValidation(t *testing.T) {
	if _, err := New(Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("New: %v", err)
	}
	s := mustStore(t, 64)
	if _, err := s.Put("", nil); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.Range("a", "a", 0); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("Range: %v", err)
	}
	if _, err := s.Watch("", 0, -1); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("Watch: %v", err)
	}
}

func TestPutRangeHistoryAndOwnership(t *testing.T) {
	s := mustStore(t, 64)
	in := []byte("one")
	e1, err := s.Put("a", in)
	if err != nil {
		t.Fatal(err)
	}
	in[0] = 'X'
	e2, err := s.Put("a", []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	if e1.Revision != 1 || e1.KV.CreateRevision != 1 || e1.KV.Version != 1 {
		t.Fatalf("e1=%+v", e1)
	}
	if e2.Revision != 2 || e2.KV.CreateRevision != 1 || e2.KV.Version != 2 || string(e2.Prev.Value) != "one" {
		t.Fatalf("e2=%+v", e2)
	}
	old, err := s.Range("a", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	now, err := s.Range("a", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(old[0].Value) != "one" || string(now[0].Value) != "two" {
		t.Fatalf("old=%q now=%q", old[0].Value, now[0].Value)
	}
	now[0].Value[0] = 'Z'
	again, _ := s.Range("a", "", 0)
	if string(again[0].Value) != "two" {
		t.Fatal("returned value aliases store")
	}
	if _, err := s.Range("a", "", 3); !errors.Is(err, ErrFutureRevision) {
		t.Fatalf("future: %v", err)
	}
}

func TestDeleteRecreateAndWatch(t *testing.T) {
	s := mustStore(t, 128)
	s.Put("p/a", []byte("1"))
	s.Put("x", []byte("x"))
	del, ok, err := s.Delete("p/a")
	if err != nil || !ok {
		t.Fatalf("delete: %v %t", err, ok)
	}
	if del.Type != EventDelete || del.Revision != 3 || del.KV.Version != 1 || string(del.Prev.Value) != "1" {
		t.Fatalf("del=%+v", del)
	}
	if _, ok, err := s.Delete("p/a"); err != nil || ok {
		t.Fatalf("noop delete: %v %t", err, ok)
	}
	re, _ := s.Put("p/a", []byte("2"))
	if re.KV.CreateRevision != 4 || re.KV.Version != 1 {
		t.Fatalf("recreate=%+v", re)
	}
	events, err := s.Watch("p/", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Revision != 1 || events[1].Revision != 3 {
		t.Fatalf("events=%+v", events)
	}
	events[0].KV.Value[0] = 'Q'
	again, _ := s.Watch("p/", 0, 1)
	if string(again[0].KV.Value) != "1" {
		t.Fatal("event aliases store")
	}
}

func TestTxnReadYourWritesOneRevisionAndOrdering(t *testing.T) {
	s := mustStore(t, 128)
	s.Put("a", []byte("old"))
	r, err := s.Txn(
		[]Compare{{Key: "a", Target: CompareValue, Result: CompareEqual, Value: []byte("old")}},
		[]Op{{Type: OpPut, Key: "b", Value: []byte("B")}, {Type: OpPut, Key: "a", Value: []byte("new")}, {Type: OpRange, Key: "a", End: "c"}, {Type: OpDelete, Key: "b"}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Succeeded || r.Revision != 2 || len(r.Events) != 3 || len(r.Responses) != 4 {
		t.Fatalf("r=%+v", r)
	}
	for i, e := range r.Events {
		if e.Revision != 2 || e.Sequence != uint32(i) {
			t.Fatalf("event[%d]=%+v", i, e)
		}
	}
	if got := r.Responses[2].KVs; len(got) != 2 || got[0].Key != "a" || string(got[0].Value) != "new" || got[1].Key != "b" {
		t.Fatalf("range=%+v", got)
	}
	if r.Responses[3].Deleted != 1 {
		t.Fatalf("delete response=%+v", r.Responses[3])
	}
	snap := s.Snapshot()
	if snap.Revision != 2 || len(snap.KVs) != 1 || snap.KVs[0].Key != "a" {
		t.Fatalf("snap=%+v", snap)
	}
}

func TestTxnFailureBranchAndReadOnly(t *testing.T) {
	s := mustStore(t, 64)
	r, err := s.Txn([]Compare{{Key: "missing", Target: CompareExists, Result: CompareEqual, Revision: 1}}, nil, []Op{{Type: OpPut, Key: "fallback", Value: []byte("yes")}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Succeeded || r.Revision != 1 || len(r.Events) != 1 {
		t.Fatalf("r=%+v", r)
	}
	r, err = s.Txn(nil, []Op{{Type: OpRange, Key: "a", End: "z"}}, nil)
	if err != nil || r.Revision != 1 || len(r.Events) != 0 || len(r.Responses) != 1 {
		t.Fatalf("readonly=%+v err=%v", r, err)
	}
}

func TestTxnRollbackAndFinalCapacity(t *testing.T) {
	s := mustStore(t, 8)
	s.Put("a", []byte("123")) // 4 live bytes
	r, err := s.Txn(nil, []Op{{Type: OpPut, Key: "bbbb", Value: []byte("12345")}, {Type: OpDelete, Key: "bbbb"}, {Type: OpPut, Key: "c", Value: []byte("12")}}, nil)
	if err != nil || r.Revision != 2 {
		t.Fatalf("temporary capacity: r=%+v err=%v", r, err)
	}
	before := s.Snapshot()
	_, err = s.Txn(nil, []Op{{Type: OpPut, Key: "long", Value: []byte("overflow")}}, nil)
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	after := s.Snapshot()
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("mutated on failure: before=%+v after=%+v", before, after)
	}
	_, err = s.Txn(nil, nil, []Op{{Type: 99, Key: "x"}})
	if !errors.Is(err, ErrInvalidOp) {
		t.Fatalf("unselected branch must validate: %v", err)
	}
}

func TestRangeDeleteAndHistoricalView(t *testing.T) {
	s := mustStore(t, 128)
	s.Put("a", []byte("1"))
	s.Put("b", []byte("2"))
	s.Put("c", []byte("3"))
	r, err := s.Txn(nil, []Op{{Type: OpDelete, Key: "a", End: "c"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Responses[0].Deleted != 2 || len(r.Events) != 2 || r.Events[0].KV.Key != "a" || r.Events[1].KV.Key != "b" {
		t.Fatalf("r=%+v", r)
	}
	old, _ := s.Range("a", "d", 3)
	if len(old) != 3 {
		t.Fatalf("old=%+v", old)
	}
}

func TestCompactionBoundaries(t *testing.T) {
	s := mustStore(t, 128)
	s.Put("a", []byte("1"))
	s.Put("a", []byte("2"))
	s.Delete("a")
	s.Put("a", []byte("3"))
	if err := s.Compact(2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Range("a", "", 2); !errors.Is(err, ErrCompacted) {
		t.Fatalf("range compacted: %v", err)
	}
	if _, err := s.Watch("", 1, 0); !errors.Is(err, ErrCompacted) {
		t.Fatalf("watch compacted: %v", err)
	}
	events, err := s.Watch("", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Revision != 3 || events[1].Revision != 4 {
		t.Fatalf("events=%+v", events)
	}
	if err := s.Compact(1); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("backward: %v", err)
	}
	if err := s.Compact(5); !errors.Is(err, ErrFutureRevision) {
		t.Fatalf("future: %v", err)
	}
	cur, err := s.Range("a", "", 0)
	if err != nil || len(cur) != 1 || string(cur[0].Value) != "3" {
		t.Fatalf("cur=%+v err=%v", cur, err)
	}
}

func TestConcurrentCalls(t *testing.T) {
	s := mustStore(t, 1<<20)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				key := fmt.Sprintf("k/%d/%d", g, i)
				if _, err := s.Put(key, []byte{byte(i)}); err != nil {
					t.Errorf("put: %v", err)
				}
				if _, err := s.Range("k/", "k0", 0); err != nil {
					t.Errorf("range: %v", err)
				}
				_ = s.Snapshot()
			}
		}()
	}
	wg.Wait()
	snap := s.Snapshot()
	if len(snap.KVs) != 800 || snap.Revision != 800 {
		t.Fatalf("snap keys=%d rev=%d", len(snap.KVs), snap.Revision)
	}
}

func TestCompareOrdering(t *testing.T) {
	s := mustStore(t, 64)
	s.Put("a", []byte("m"))
	cases := []Compare{
		{Key: "a", Target: CompareValue, Result: CompareLess, Value: []byte("z")},
		{Key: "a", Target: CompareModRevision, Result: CompareEqual, Revision: 1},
		{Key: "a", Target: CompareVersion, Result: CompareGreater, Revision: 0},
		{Key: "missing", Target: CompareExists, Result: CompareEqual, Revision: 0},
	}
	r, err := s.Txn(cases, []Op{{Type: OpRange, Key: "a"}}, nil)
	if err != nil || !r.Succeeded {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if !bytes.Equal(r.Responses[0].KVs[0].Value, []byte("m")) {
		t.Fatal("bad value")
	}
}
