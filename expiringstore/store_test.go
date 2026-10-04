package expiringstore

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestExpirationRollbackOnFailure(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Now: 1, Ops: []Op{put("a", "x", 3), put("b", "y", 10)}}); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	// Batch at Now=5 would expire "a", but Delete of missing key must roll back
	// the expiration, time advance, generation, and revision allocation.
	_, e := s.Apply(Batch{Now: 5, Ops: []Op{
		put("c", "z", 9),
		{Kind: Delete, Key: "ghost"},
	}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatalf("e=%v", e)
	}
	if after := s.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("state leaked:\nbefore=%+v\nafter=%+v", before, after)
	}
	// "a" must still be retrievable before its expiry.
	if _, ok, e := s.Get("a", 2); e != nil || !ok {
		t.Fatalf("ok=%v e=%v", ok, e)
	}
}

func TestCapacityRollbackRestoresExpired(t *testing.T) {
	s, _ := New(Options{MaxEntries: 2, MaxTotalBytes: 8, MaxValueBytes: 4, MaxKeyBytes: 8})
	if _, e := s.Apply(Batch{Now: 1, Ops: []Op{put("a", "aa", 2), put("b", "bb", 9)}}); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	// Expiring "a" frees room for one entry, but two puts overflow capacity;
	// the whole batch including the expiration of "a" must roll back.
	_, e := s.Apply(Batch{Now: 2, Ops: []Op{put("c", "cc", 9), put("d", "dd", 9)}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if after := s.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("state leaked:\nbefore=%+v\nafter=%+v", before, after)
	}
}

func TestRepeatedWritesRevisionSequence(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Now: 1, Ops: []Op{
		put("a", "v1", 10),
		put("a", "v2", 11),
		put("b", "w1", 10),
		put("a", "v3", 12),
	}})
	if e != nil || r.Revision != 4 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	got, ok, e := s.Get("a", 1)
	if e != nil || !ok || string(got.Value) != "v3" || got.Revision != 4 || got.ExpiresAt != 12 {
		t.Fatalf("got=%+v ok=%v e=%v", got, ok, e)
	}
	// Revision allocation continues across batches without reuse.
	r, e = s.Apply(Batch{Now: 2, Ops: []Op{put("c", "z", 10)}})
	if e != nil || r.Revision != 5 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	x := s.Snapshot()
	if x.NextRevision != 6 {
		t.Fatalf("NextRevision=%d", x.NextRevision)
	}
}

func TestTouchAndDeleteSemantics(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Now: 1, Ops: []Op{put("a", "x", 5)}}); e != nil {
		t.Fatal(e)
	}
	// Touch changes only expiry, not value or revision.
	if _, e := s.Apply(Batch{Now: 2, Ops: []Op{{Kind: Touch, Key: "a", ExpiresAt: 20}}}); e != nil {
		t.Fatal(e)
	}
	got, ok, _ := s.Get("a", 6)
	if !ok || string(got.Value) != "x" || got.Revision != 1 || got.ExpiresAt != 20 {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	// Touch of missing key fails and rolls back.
	before := s.Snapshot()
	if _, e := s.Apply(Batch{Now: 7, Ops: []Op{{Kind: Touch, Key: "nope", ExpiresAt: 30}}}); !errors.Is(e, ErrNotFound) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("touch rollback leaked")
	}
	// Delete removes the key; a second Delete in the same batch fails.
	if _, e := s.Apply(Batch{Now: 8, Ops: []Op{{Kind: Delete, Key: "a"}}}); e != nil {
		t.Fatal(e)
	}
	if _, ok, _ := s.Get("a", 8); ok {
		t.Fatal("deleted key still present")
	}
	before = s.Snapshot()
	if _, e := s.Apply(Batch{Now: 9, Ops: []Op{{Kind: Delete, Key: "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("delete rollback leaked")
	}
}

func TestTouchThenPutSameBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Now: 1, Ops: []Op{
		put("a", "x", 5),
		{Kind: Touch, Key: "a", ExpiresAt: 50},
		put("a", "y", 60),
		{Kind: Delete, Key: "a"},
		put("a", "z", 70),
	}})
	if e != nil || r.Revision != 3 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	got, ok, _ := s.Get("a", 1)
	if !ok || string(got.Value) != "z" || got.ExpiresAt != 70 || got.Revision != 3 {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
}

func TestExpiryBoundaryAndResultExpired(t *testing.T) {
	s := store(t)
	// ExpiresAt == Now is invalid input; ExpiresAt == Now+1 is the minimum.
	if _, e := s.Apply(Batch{Now: 5, Ops: []Op{put("a", "x", 5)}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
	r, e := s.Apply(Batch{Now: 5, Ops: []Op{put("a", "x", 6), put("b", "y", 7), put("c", "z", 8)}})
	if e != nil {
		t.Fatal(e)
	}
	// Entry is live at ExpiresAt-1, expired exactly at ExpiresAt.
	r, e = s.Apply(Batch{Now: 7, Ops: nil})
	if e != nil || !reflect.DeepEqual(r.Expired, []string{"a", "b"}) {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	if r.Generation != 2 {
		t.Fatalf("gen=%d", r.Generation)
	}
	// Empty batch that only advances time does not bump generation.
	r, e = s.Apply(Batch{Now: 100, Ops: nil})
	if e != nil || r.Generation != 3 || len(r.Expired) != 1 || r.Expired[0] != "c" {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	r, e = s.Apply(Batch{Now: 200})
	if e != nil || r.Generation != 3 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	if x := s.Snapshot(); x.Now != 200 || x.Generation != 3 {
		t.Fatalf("s=%+v", x)
	}
}

func TestTimeMonotonicityAllMethods(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Now: 10, Ops: []Op{put("a", "x", 20)}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Now: 9}); !errors.Is(e, ErrTime) {
		t.Fatalf("apply e=%v", e)
	}
	if _, _, e := s.Get("a", 9); !errors.Is(e, ErrTime) {
		t.Fatalf("get e=%v", e)
	}
	if _, e := s.Sweep(9); !errors.Is(e, ErrTime) {
		t.Fatalf("sweep e=%v", e)
	}
	// Equal time is allowed.
	if _, _, e := s.Get("a", 10); e != nil {
		t.Fatalf("get e=%v", e)
	}
	if _, e := s.Sweep(10); e != nil {
		t.Fatalf("sweep e=%v", e)
	}
	if x := s.Snapshot(); x.Now != 10 {
		t.Fatalf("now=%d", x.Now)
	}
}

func TestGetValidatesKeyBeforeTime(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad key!", 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
	if _, _, e := s.Get("", 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
	long := make([]byte, 17)
	for i := range long {
		long[i] = 'a'
	}
	if _, _, e := s.Get(string(long), 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
}

func TestValidationErrors(t *testing.T) {
	s := store(t)
	cases := []Op{
		{Kind: Put, Key: "a", Value: nil, ExpiresAt: 5},                    // nil value
		{Kind: Put, Key: "a", Value: make([]byte, 17), ExpiresAt: 5},       // oversize value
		{Kind: Put, Key: "a", Value: []byte("x"), ExpiresAt: 0},            // expiry not after now
		{Kind: Delete, Key: "a", Value: []byte("x")},                       // delete with value
		{Kind: Delete, Key: "a", ExpiresAt: 3},                             // delete with expiry
		{Kind: Touch, Key: "a", Value: []byte("x"), ExpiresAt: 3},          // touch with value
		{Kind: Touch, Key: "a", ExpiresAt: 0},                              // touch expiry not after now
		{Kind: OpKind(99), Key: "a"},                                       // unknown kind
		{Kind: Put, Key: "bad key", Value: []byte("x"), ExpiresAt: 5},      // bad key chars
		{Kind: Put, Key: "", Value: []byte("x"), ExpiresAt: 5},             // empty key
	}
	for i, op := range cases {
		if _, e := s.Apply(Batch{Now: 1, Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: e=%v", i, e)
		}
	}
	if x := s.Snapshot(); x.Now != 0 || len(x.Entries) != 0 || x.Generation != 0 {
		t.Fatalf("failed validation mutated state: %+v", x)
	}
}

func TestCapacityTotalBytesAndReplace(t *testing.T) {
	s, _ := New(Options{MaxEntries: 10, MaxTotalBytes: 6, MaxValueBytes: 6, MaxKeyBytes: 8})
	if _, e := s.Apply(Batch{Now: 1, Ops: []Op{put("a", "aaa", 9), put("b", "bbb", 9)}}); e != nil {
		t.Fatal(e)
	}
	// Replacing "a" with a same-size value stays within total bytes.
	if _, e := s.Apply(Batch{Now: 2, Ops: []Op{put("a", "ccc", 9)}}); e != nil {
		t.Fatal(e)
	}
	// Growing beyond total bytes fails and rolls back.
	before := s.Snapshot()
	if _, e := s.Apply(Batch{Now: 3, Ops: []Op{put("a", "cccc", 9)}}); !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("capacity rollback leaked")
	}
	if x := s.Snapshot(); x.TotalBytes != 6 {
		t.Fatalf("total=%d", x.TotalBytes)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	s := store(t)
	in := []byte("abc")
	if _, e := s.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "k", Value: in, ExpiresAt: 9}}}); e != nil {
		t.Fatal(e)
	}
	in[0] = 'X' // mutating caller input must not affect stored value
	got, ok, _ := s.Get("k", 1)
	if !ok || string(got.Value) != "abc" {
		t.Fatalf("got=%q ok=%v", got.Value, ok)
	}
	got.Value[0] = 'Y' // mutating Get output must not affect stored value
	snap := s.Snapshot()
	if string(snap.Entries[0].Value) != "abc" {
		t.Fatalf("snap=%q", snap.Entries[0].Value)
	}
	snap.Entries[0].Value[0] = 'Z' // mutating Snapshot output must not leak
	got2, _, _ := s.Get("k", 1)
	if string(got2.Value) != "abc" {
		t.Fatalf("got2=%q", got2.Value)
	}
}

func TestGetExpiresTargetKeyAtomically(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Now: 1, Ops: []Op{put("a", "x", 5), put("b", "y", 5)}}); e != nil {
		t.Fatal(e)
	}
	// Get advances time and expires all due entries, not just the queried key.
	if _, ok, e := s.Get("a", 5); e != nil || ok {
		t.Fatalf("ok=%v e=%v", ok, e)
	}
	if _, ok, _ := s.Get("b", 5); ok {
		t.Fatal("b should have been expired by previous Get")
	}
	if x := s.Snapshot(); x.Generation != 2 || len(x.Entries) != 0 {
		t.Fatalf("s=%+v", x)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxEntries: 256, MaxTotalBytes: 4096, MaxValueBytes: 16, MaxKeyBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("key-%02d", i%16)
			for j := 0; j < 20; j++ {
				now := int64(j + 1)
				_, _ = s.Apply(Batch{Now: now, Ops: []Op{
					put(k, "v", now+100),
					{Kind: Touch, Key: k, ExpiresAt: now + 200},
				}})
				_, _, _ = s.Get(k, now)
				_, _ = s.Sweep(now)
				_ = s.Snapshot()
			}
		}()
	}
	wg.Wait()
	x := s.Snapshot()
	if x.Now != 20 {
		t.Fatalf("now=%d", x.Now)
	}
	// Revisions must be strictly increasing and unique across live entries.
	seen := map[uint64]bool{}
	for _, e := range x.Entries {
		if e.Revision == 0 || e.Revision >= x.NextRevision || seen[e.Revision] {
			t.Fatalf("bad revision %d next=%d", e.Revision, x.NextRevision)
		}
		seen[e.Revision] = true
	}
}
