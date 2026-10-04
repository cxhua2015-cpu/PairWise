package expiringstore

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func options() Options { return Options{MaxEntries: 8, MaxTotalBytes: 64, MaxValueBytes: 16, MaxKeyBytes: 16} }
func store(t *testing.T) *Store { t.Helper(); s, e := New(options()); if e != nil { t.Fatal(e) }; return s }
func put(k, v string, exp int64) Op { return Op{Kind: Put, Key: k, Value: []byte(v), ExpiresAt: exp} }

func TestValidationBeforeState(t *testing.T) {
	if _, e := New(Options{}); !errors.Is(e, ErrInvalidOptions) { t.Fatal(e) }
	s := store(t)
	_, _ = s.Apply(Batch{Now: 1, Ops: []Op{put("a", "x", 2)}})
	before := s.Snapshot()
	_, e := s.Apply(Batch{Now: 3, Ops: []Op{{Kind: Delete, Key: "a"}, {Kind: Put, Key: "bad?", Value: []byte("x"), ExpiresAt: 4}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(before, s.Snapshot()) { t.Fatalf("e=%v", e) }
}

func TestExpiryBoundaryGetAndSweep(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Now: 1, Ops: []Op{put("a", "one", 5), put("b", "two", 6)}})
	if e != nil || r.Revision != 2 || r.Generation != 1 { t.Fatalf("r=%+v e=%v", r, e) }
	if _, ok, e := s.Get("a", 4); e != nil || !ok { t.Fatalf("ok=%v e=%v", ok, e) }
	if _, ok, e := s.Get("a", 5); e != nil || ok { t.Fatalf("ok=%v e=%v", ok, e) }
	exp, e := s.Sweep(6)
	if e != nil || !reflect.DeepEqual(exp, []string{"b"}) { t.Fatalf("expired=%v e=%v", exp, e) }
	x := s.Snapshot(); if x.Generation != 3 || x.Now != 6 || len(x.Entries) != 0 { t.Fatalf("s=%+v", x) }
}

func TestSequentialBatchAndRollback(t *testing.T) {
	s, _ := New(Options{MaxEntries: 2, MaxTotalBytes: 4, MaxValueBytes: 4, MaxKeyBytes: 8})
	r, e := s.Apply(Batch{Now: 1, Ops: []Op{put("a", "a", 3), put("a", "bb", 4), {Kind: Touch, Key: "a", ExpiresAt: 5}}})
	if e != nil || r.Revision != 2 { t.Fatalf("r=%+v e=%v", r, e) }
	before := s.Snapshot()
	_, e = s.Apply(Batch{Now: 5, Ops: []Op{put("b", "bbbb", 9), {Kind: Delete, Key: "missing"}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, s.Snapshot()) { t.Fatalf("e=%v s=%+v", e, s.Snapshot()) }
	_, e = s.Apply(Batch{Now: 0, Ops: nil}); if !errors.Is(e, ErrTime) { t.Fatal(e) }
}

func TestCapacityOwnershipAndSnapshotOrder(t *testing.T) {
	s := store(t); value := []byte("one")
	_, _ = s.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "z", Value: value, ExpiresAt: 9}, put("a", "two", 9)}})
	value[0] = 'X'
	x := s.Snapshot(); if len(x.Entries) != 2 || x.Entries[0].Key != "a" || string(x.Entries[1].Value) != "one" { t.Fatalf("s=%+v", x) }
	x.Entries[1].Value[0] = 'Y'; got, _, _ := s.Get("z", 1); if string(got.Value) != "one" { t.Fatal("alias") }
	small, _ := New(Options{MaxEntries: 1, MaxTotalBytes: 2, MaxValueBytes: 2, MaxKeyBytes: 8})
	before := small.Snapshot(); _, e := small.Apply(Batch{Now: 1, Ops: []Op{put("a", "aa", 8), put("b", "", 8)}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, small.Snapshot()) { t.Fatalf("e=%v", e) }
}

func TestConcurrentCalls(t *testing.T) {
	s, _ := New(Options{MaxEntries: 128, MaxTotalBytes: 1024, MaxValueBytes: 8, MaxKeyBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 48; i++ { i := i; wg.Add(1); go func(){ defer wg.Done(); k := fmt.Sprintf("k-%02d", i); _, _ = s.Apply(Batch{Now: 1, Ops: []Op{put(k, "x", 10)}}); _, _, _ = s.Get(k, 1); _ = s.Snapshot() }() }
	wg.Wait(); if len(s.Snapshot().Entries) != 48 { t.Fatalf("s=%+v", s.Snapshot()) }
}
