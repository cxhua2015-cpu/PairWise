package mvcc

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestPutCapacityAndReplace(t *testing.T) {
	s := mustStore(t, 4)
	if _, err := s.Put("ab", []byte("cd")); err != nil { // exactly 4
		t.Fatal(err)
	}
	if _, err := s.Put("e", nil); !errors.Is(err, ErrCapacity) {
		t.Fatalf("expect capacity, got %v", err)
	}
	// replacing with same-size value fits
	if _, err := s.Put("ab", []byte("XY")); err != nil {
		t.Fatal(err)
	}
	// replacing with larger value exceeds
	if _, err := s.Put("ab", []byte("XYZ")); !errors.Is(err, ErrCapacity) {
		t.Fatalf("expect capacity, got %v", err)
	}
	kvs, _ := s.Range("ab", "", 0)
	if string(kvs[0].Value) != "XY" {
		t.Fatalf("state mutated on capacity error: %q", kvs[0].Value)
	}
	if s.Snapshot().Revision != 2 {
		t.Fatal("revision allocated on failed put")
	}
}

func TestRangeBoundariesAndSorting(t *testing.T) {
	s := mustStore(t, 1<<20)
	for _, k := range []string{"b", "a", "c", "ab", "abc"} {
		if _, err := s.Put(k, []byte(k)); err != nil {
			t.Fatal(err)
		}
	}
	kvs, err := s.Range("a", "c", 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, kv := range kvs {
		got = append(got, kv.Key)
	}
	want := []string{"a", "ab", "abc", "b"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if _, err := s.Range("", "", 0); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("empty key: %v", err)
	}
	if _, err := s.Range("b", "a", 0); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("reversed: %v", err)
	}
}

func TestDeleteTombstoneEvent(t *testing.T) {
	s := mustStore(t, 64)
	s.Put("k", []byte("v1"))
	s.Put("k", []byte("v2"))
	e, ok, err := s.Delete("k")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if e.Type != EventDelete || e.KV.Value != nil || e.KV.CreateRevision != 1 ||
		e.KV.ModRevision != 3 || e.KV.Version != 2 || string(e.Prev.Value) != "v2" {
		t.Fatalf("e=%+v", e)
	}
	if kvs, _ := s.Range("k", "", 0); len(kvs) != 0 {
		t.Fatal("key still live")
	}
	// history at rev 2 still readable
	old, err := s.Range("k", "", 2)
	if err != nil || len(old) != 1 || string(old[0].Value) != "v2" {
		t.Fatalf("old=%+v err=%v", old, err)
	}
}

func TestWatchPrefixLimitAndFuture(t *testing.T) {
	s := mustStore(t, 1<<20)
	s.Put("a/1", []byte("x"))
	s.Put("b/1", []byte("x"))
	s.Put("a/2", []byte("x"))
	all, err := s.Watch("", 0, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("all=%+v err=%v", all, err)
	}
	pref, _ := s.Watch("a/", 0, 0)
	if len(pref) != 2 {
		t.Fatalf("pref=%+v", pref)
	}
	one, _ := s.Watch("", 1, 1)
	if len(one) != 1 || one[0].Revision != 2 {
		t.Fatalf("one=%+v", one)
	}
	if _, err := s.Watch("", 4, 0); !errors.Is(err, ErrFutureRevision) {
		t.Fatalf("future: %v", err)
	}
}

func TestTxnValidationDoesNotMutate(t *testing.T) {
	s := mustStore(t, 64)
	s.Put("a", []byte("1"))
	before := s.Snapshot()
	bad := []Op{{Type: OpPut, Key: "x", Revision: 1}}
	if _, err := s.Txn(nil, bad, nil); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("rev: %v", err)
	}
	if _, err := s.Txn(nil, []Op{{Type: OpPut, Key: "x", End: "y"}}, nil); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("put with end: %v", err)
	}
	if _, err := s.Txn(nil, []Op{{Type: OpRange, Key: "a", Revision: 2}}, nil); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("range rev: %v", err)
	}
	if _, err := s.Txn([]Compare{{Key: "a", Target: 99, Result: CompareEqual}}, nil, nil); !errors.Is(err, ErrInvalidCompare) {
		t.Fatalf("target: %v", err)
	}
	if _, err := s.Txn([]Compare{{Key: "a", Target: CompareExists, Result: CompareLess}}, nil, nil); !errors.Is(err, ErrInvalidCompare) {
		t.Fatalf("exists less: %v", err)
	}
	if _, err := s.Txn([]Compare{{Key: "", Target: CompareExists, Result: CompareEqual}}, nil, nil); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("empty compare key: %v", err)
	}
	after := s.Snapshot()
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatal("validation error mutated state")
	}
}

func TestTxnNoopDeleteKeepsRevision(t *testing.T) {
	s := mustStore(t, 64)
	r, err := s.Txn(nil, []Op{{Type: OpDelete, Key: "ghost"}}, nil)
	if err != nil || r.Revision != 0 || len(r.Events) != 0 || r.Responses[0].Deleted != 0 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if s.Snapshot().Revision != 0 {
		t.Fatal("noop txn allocated revision")
	}
}

func TestCompactionRetainsBaseVersion(t *testing.T) {
	s := mustStore(t, 1<<20)
	s.Put("k", []byte("v1")) // rev 1
	s.Put("k", []byte("v2")) // rev 2
	s.Put("k", []byte("v3")) // rev 3
	if err := s.Compact(2); err != nil {
		t.Fatal(err)
	}
	// base version at rev 2 retained: read at rev 3 sees v2's successor
	kvs, err := s.Range("k", "", 3)
	if err != nil || len(kvs) != 1 || string(kvs[0].Value) != "v3" {
		t.Fatalf("kvs=%+v err=%v", kvs, err)
	}
	if _, err := s.Range("k", "", 2); !errors.Is(err, ErrCompacted) {
		t.Fatalf("compacted read: %v", err)
	}
	// repeating current compact revision is fine
	if err := s.Compact(2); err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(0); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("zero: %v", err)
	}
	snap := s.Snapshot()
	if snap.CompactRevision != 2 || snap.Revision != 3 {
		t.Fatalf("snap=%+v", snap)
	}
}

func TestPayloadOwnership(t *testing.T) {
	s := mustStore(t, 64)
	in := []byte("abc")
	e, _ := s.Put("k", in)
	in[0] = 'z'
	if string(e.KV.Value) != "abc" {
		t.Fatal("event aliases input")
	}
	e.KV.Value[0] = 'z'
	e.Prev = &KV{Value: []byte("x")}
	kvs, _ := s.Range("k", "", 0)
	if string(kvs[0].Value) != "abc" {
		t.Fatal("store aliases returned event")
	}
	kvs[0].Value[0] = 'z'
	snap := s.Snapshot()
	if string(snap.KVs[0].Value) != "abc" {
		t.Fatal("store aliases returned range kv")
	}
	snap.KVs[0].Value[0] = 'z'
	again, _ := s.Range("k", "", 0)
	if string(again[0].Value) != "abc" {
		t.Fatal("store aliases snapshot kv")
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
				_, err := s.Txn(nil, []Op{
					{Type: OpPut, Key: key, Value: []byte{byte(i)}},
					{Type: OpRange, Key: "t/", End: "t0"},
					{Type: OpDelete, Key: "t/none", End: "t/none2"},
				}, nil)
				if err != nil {
					t.Errorf("txn: %v", err)
				}
				if _, err := s.Watch("t/", 0, 10); err != nil && !errors.Is(err, ErrCompacted) {
					t.Errorf("watch: %v", err)
				}
				if err := s.Compact(1); err != nil && !errors.Is(err, ErrInvalidRevision) {
					t.Errorf("compact: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	snap := s.Snapshot()
	if len(snap.KVs) != 200 || snap.Revision != 200 {
		t.Fatalf("snap keys=%d rev=%d", len(snap.KVs), snap.Revision)
	}
	// sequences within each revision must be dense from 0
	events, err := s.Watch("", snap.CompactRevision, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Sequence != 0 {
			t.Fatalf("bad sequence: %+v", e)
		}
	}
}
