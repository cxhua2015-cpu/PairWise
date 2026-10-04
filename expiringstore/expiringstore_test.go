package expiringstore

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func newStore(t *testing.T, o Options) *Store {
	t.Helper()
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewOptionsValidation(t *testing.T) {
	for _, o := range []Options{
		{},
		{MaxEntries: 1, MaxTotalBytes: 1, MaxValueBytes: 1},
		{MaxEntries: 1, MaxTotalBytes: 1, MaxValueBytes: 1, MaxKeyBytes: 0},
		{MaxEntries: 1, MaxTotalBytes: 1, MaxValueBytes: 2, MaxKeyBytes: 1},
		{MaxEntries: -1, MaxTotalBytes: 1, MaxValueBytes: 1, MaxKeyBytes: 1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts=%+v err=%v", o, err)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 4, MaxTotalBytes: 16, MaxValueBytes: 4, MaxKeyBytes: 16})
	for _, key := range []string{"", "bad key", "bad?", "thiskeyisfartoolong", "é"} {
		_, err := s.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: key, Value: []byte("v"), ExpiresAt: 9}}})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key=%q err=%v", key, err)
		}
		if _, _, err := s.Get(key, 1); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("get key=%q err=%v", key, err)
		}
	}
	if _, err := s.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "a.b_c/d-e", Value: []byte("v"), ExpiresAt: 9}}}); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedOps(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 4, MaxTotalBytes: 16, MaxValueBytes: 4, MaxKeyBytes: 8})
	cases := []Op{
		{Kind: Put, Key: "a", Value: nil, ExpiresAt: 9},
		{Kind: Put, Key: "a", Value: []byte("12345"), ExpiresAt: 9},
		{Kind: Put, Key: "a", Value: []byte("v"), ExpiresAt: 1},
		{Kind: Delete, Key: "a", Value: []byte("v")},
		{Kind: Delete, Key: "a", ExpiresAt: 9},
		{Kind: Touch, Key: "a", Value: []byte("v"), ExpiresAt: 9},
		{Kind: Touch, Key: "a", ExpiresAt: 1},
		{Kind: OpKind(0), Key: "a"},
		{Kind: OpKind(99), Key: "a"},
	}
	for _, op := range cases {
		if _, err := s.Apply(Batch{Now: 1, Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op=%+v err=%v", op, err)
		}
	}
}

func TestRollbackRestoresExpiredEntries(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 4, MaxTotalBytes: 16, MaxValueBytes: 4, MaxKeyBytes: 8})
	if _, err := s.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "old", Value: []byte("v"), ExpiresAt: 3}}}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	// At Now=5 "old" would expire, but the batch fails on a missing Delete.
	_, err := s.Apply(Batch{Now: 5, Ops: []Op{{Kind: Delete, Key: "ghost"}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("before=%+v after=%+v", before, got)
	}
	// Time must not have advanced: Now=4 is still legal and "old" still alive.
	if _, ok, err := s.Get("old", 2); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestCapacityRollbackKeepsRevisionAndGeneration(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 2, MaxTotalBytes: 4, MaxValueBytes: 4, MaxKeyBytes: 8})
	if _, err := s.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "a", Value: []byte("aa"), ExpiresAt: 9}}}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	_, err := s.Apply(Batch{Now: 2, Ops: []Op{
		{Kind: Put, Key: "b", Value: []byte("bb"), ExpiresAt: 9},
		{Kind: Put, Key: "c", Value: []byte("cc"), ExpiresAt: 9},
	}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("before=%+v after=%+v", before, got)
	}
	// Revision allocation rolled back: next Put gets revision 2.
	r, err := s.Apply(Batch{Now: 2, Ops: []Op{{Kind: Put, Key: "b", Value: []byte("bb"), ExpiresAt: 9}}})
	if err != nil || r.Revision != 2 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
}

func TestRepeatedWritesAndRevision(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 4, MaxTotalBytes: 16, MaxValueBytes: 4, MaxKeyBytes: 8})
	r, err := s.Apply(Batch{Now: 1, Ops: []Op{
		{Kind: Put, Key: "a", Value: []byte("v1"), ExpiresAt: 9},
		{Kind: Put, Key: "a", Value: []byte("v2"), ExpiresAt: 8},
		{Kind: Put, Key: "b", Value: []byte("w"), ExpiresAt: 9},
	}})
	if err != nil || r.Revision != 3 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	snap := s.Snapshot()
	if snap.TotalBytes != 3 || len(snap.Entries) != 2 {
		t.Fatalf("snap=%+v", snap)
	}
	e, ok, err := s.Get("a", 1)
	if err != nil || !ok || string(e.Value) != "v2" || e.Revision != 2 || e.ExpiresAt != 8 {
		t.Fatalf("e=%+v ok=%v err=%v", e, ok, err)
	}
}

func TestTouchAndDelete(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 4, MaxTotalBytes: 16, MaxValueBytes: 4, MaxKeyBytes: 8})
	if _, err := s.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "a", Value: []byte("v"), ExpiresAt: 5}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Now: 2, Ops: []Op{{Kind: Touch, Key: "a", ExpiresAt: 10}}}); err != nil {
		t.Fatal(err)
	}
	e, ok, _ := s.Get("a", 6)
	if !ok || e.ExpiresAt != 10 {
		t.Fatalf("e=%+v ok=%v", e, ok)
	}
	if _, err := s.Apply(Batch{Now: 6, Ops: []Op{{Kind: Delete, Key: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get("a", 6); ok {
		t.Fatal("expected deleted")
	}
	// Touch/Delete of missing keys fail and roll back.
	for _, op := range []Op{{Kind: Touch, Key: "a", ExpiresAt: 9}, {Kind: Delete, Key: "a"}} {
		if _, err := s.Apply(Batch{Now: 7, Ops: []Op{op}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("op=%+v err=%v", op, err)
		}
	}
}

func TestDeleteThenPutSameBatch(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 4, MaxTotalBytes: 16, MaxValueBytes: 4, MaxKeyBytes: 8})
	r, err := s.Apply(Batch{Now: 1, Ops: []Op{
		{Kind: Put, Key: "a", Value: []byte("v"), ExpiresAt: 9},
		{Kind: Delete, Key: "a"},
		{Kind: Put, Key: "a", Value: []byte("w"), ExpiresAt: 9},
	}})
	if err != nil || r.Revision != 2 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	e, ok, _ := s.Get("a", 1)
	if !ok || string(e.Value) != "w" || e.Revision != 2 {
		t.Fatalf("e=%+v ok=%v", e, ok)
	}
}

func TestTimeMonotonicityAcrossOps(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 4, MaxTotalBytes: 16, MaxValueBytes: 4, MaxKeyBytes: 8})
	if _, err := s.Apply(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Now: 4}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := s.Sweep(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, _, err := s.Get("a", 4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	// Equal time is allowed.
	if _, err := s.Apply(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
}

func TestGenerationSemantics(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 4, MaxTotalBytes: 16, MaxValueBytes: 4, MaxKeyBytes: 8})
	// Empty batch that only advances time: no generation bump.
	if _, err := s.Apply(Batch{Now: 3}); err != nil {
		t.Fatal(err)
	}
	if g := s.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
	// Batch with ops bumps once.
	r, _ := s.Apply(Batch{Now: 4, Ops: []Op{{Kind: Put, Key: "a", Value: []byte("v"), ExpiresAt: 6}, {Kind: Put, Key: "b", Value: []byte("w"), ExpiresAt: 6}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	// Empty batch that expires entries bumps once.
	r, _ = s.Apply(Batch{Now: 6})
	if r.Generation != 2 || !reflect.DeepEqual(r.Expired, []string{"a", "b"}) {
		t.Fatalf("r=%+v", r)
	}
	// Get with no expiry does not bump; Sweep with expiry does.
	if _, _, err := s.Get("a", 7); err != nil {
		t.Fatal(err)
	}
	if g := s.Snapshot().Generation; g != 2 {
		t.Fatal(g)
	}
	if _, err := s.Apply(Batch{Now: 7, Ops: []Op{{Kind: Put, Key: "c", Value: []byte("x"), ExpiresAt: 8}}}); err != nil {
		t.Fatal(err)
	}
	exp, err := s.Sweep(8)
	if err != nil || !reflect.DeepEqual(exp, []string{"c"}) {
		t.Fatalf("exp=%v err=%v", exp, err)
	}
	if g := s.Snapshot().Generation; g != 4 {
		t.Fatal(g)
	}
}

func TestResultRevisionZeroBeforeAnyPut(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 4, MaxTotalBytes: 16, MaxValueBytes: 4, MaxKeyBytes: 8})
	r, err := s.Apply(Batch{Now: 1})
	if err != nil || r.Revision != 0 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if n := s.Snapshot().NextRevision; n != 1 {
		t.Fatal(n)
	}
}

func TestValueOwnership(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 4, MaxTotalBytes: 16, MaxValueBytes: 4, MaxKeyBytes: 8})
	in := []byte("abc")
	if _, err := s.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "a", Value: in, ExpiresAt: 9}}}); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X'
	e, _, _ := s.Get("a", 1)
	if string(e.Value) != "abc" {
		t.Fatal("input aliased")
	}
	e.Value[0] = 'Y'
	e2, _, _ := s.Get("a", 1)
	if string(e2.Value) != "abc" {
		t.Fatal("output aliased")
	}
	snap := s.Snapshot()
	snap.Entries[0].Value[0] = 'Z'
	e3, _, _ := s.Get("a", 1)
	if string(e3.Value) != "abc" {
		t.Fatal("snapshot aliased")
	}
}

func TestExpiryBoundary(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 4, MaxTotalBytes: 16, MaxValueBytes: 4, MaxKeyBytes: 8})
	if _, err := s.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "a", Value: []byte("v"), ExpiresAt: 5}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get("a", 4); !ok {
		t.Fatal("should be alive before ExpiresAt")
	}
	// ExpiresAt <= now means expired at exactly 5.
	if _, ok, _ := s.Get("a", 5); ok {
		t.Fatal("should be expired at ExpiresAt")
	}
}

func TestConcurrentMix(t *testing.T) {
	s := newStore(t, Options{MaxEntries: 256, MaxTotalBytes: 4096, MaxValueBytes: 8, MaxKeyBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("k%02d", i)
			for n := int64(1); n <= 20; n++ {
				_, _ = s.Apply(Batch{Now: n, Ops: []Op{{Kind: Put, Key: key, Value: []byte("v"), ExpiresAt: n + 100}}})
				_, _, _ = s.Get(key, n)
				_, _ = s.Sweep(n)
				_ = s.Snapshot()
			}
		}()
	}
	wg.Wait()
	snap := s.Snapshot()
	if len(snap.Entries) != 32 || snap.Now != 20 {
		t.Fatalf("snap entries=%d now=%d", len(snap.Entries), snap.Now)
	}
	for i, e := range snap.Entries {
		if i > 0 && snap.Entries[i-1].Key >= e.Key {
			t.Fatal("entries not sorted")
		}
	}
}
