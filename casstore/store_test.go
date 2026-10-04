package casstore

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustPut(t *testing.T, s *Store, k string, v []byte) TxnResult {
	t.Helper()
	r, e := s.Transact(Txn{Writes: []Write{put(k, v)}})
	if e != nil || !r.Succeeded {
		t.Fatalf("put %s: r=%+v e=%v", k, r, e)
	}
	return r
}

func TestCompareFailures(t *testing.T) {
	s := store(t)
	r := mustPut(t, s, "x", []byte("v"))
	cases := []Compare{
		{Kind: Exists, Key: "missing"},
		{Kind: NotExists, Key: "x"},
		{Kind: Revision, Key: "x", Revision: r.Revision + 1},
		{Kind: Revision, Key: "missing", Revision: 1},
		{Kind: Value, Key: "x", Value: []byte("other")},
		{Kind: Value, Key: "missing", Value: []byte("v")},
	}
	for i, c := range cases {
		before := s.Snapshot()
		rr, e := s.Transact(Txn{Compares: []Compare{c}, Writes: []Write{put("y", []byte("y"))}})
		if e != nil || rr.Succeeded {
			t.Fatalf("case %d: r=%+v e=%v", i, rr, e)
		}
		if rr.Revision != 1 || rr.Generation != 1 {
			t.Fatalf("case %d: r=%+v", i, rr)
		}
		after := s.Snapshot()
		if after.NextRevision != before.NextRevision || after.Keys != before.Keys {
			t.Fatalf("case %d: state changed %+v -> %+v", i, before, after)
		}
	}
}

func TestCompareStructuralValidation(t *testing.T) {
	s := store(t)
	bad := []Compare{
		{Kind: Exists, Key: "x", Revision: 1},
		{Kind: Exists, Key: "x", Value: []byte{}},
		{Kind: NotExists, Key: "x", Revision: 2},
		{Kind: Revision, Key: "x"},
		{Kind: Revision, Key: "x", Revision: 1, Value: []byte{}},
		{Kind: Value, Key: "x"},
		{Kind: Value, Key: "x", Revision: 1, Value: []byte{}},
		{Kind: Value, Key: "x", Value: make([]byte, 33)},
		{Kind: 0, Key: "x"},
		{Kind: 99, Key: "x"},
		{Kind: Exists, Key: "bad key"},
		{Kind: Exists, Key: ""},
	}
	for i, c := range bad {
		if _, e := s.Transact(Txn{Compares: []Compare{c}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: e=%v", i, e)
		}
	}
	badWrites := []Write{
		{Kind: Put, Key: "x"},
		{Kind: Put, Key: "x", Value: make([]byte, 33)},
		{Kind: Delete, Key: "x", Value: []byte{}},
		{Kind: 0, Key: "x"},
		{Kind: 99, Key: "x"},
		{Kind: Put, Key: "x/y z", Value: []byte{}},
		{Kind: Delete, Key: ""},
	}
	for i, w := range badWrites {
		if _, e := s.Transact(Txn{Writes: []Write{w}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("write case %d: e=%v", i, e)
		}
	}
}

func TestRepeatedWritesAndDeleteRecreate(t *testing.T) {
	s := store(t)
	r := mustPut(t, s, "a", []byte("1"))
	// Delete then recreate in the same batch; revisions stay contiguous.
	rr, e := s.Transact(Txn{Writes: []Write{
		del("a"),
		put("a", []byte("2")),
		put("a", []byte("3")),
		del("a"),
		put("a", []byte("4")),
	}})
	if e != nil || !rr.Succeeded || rr.Revision != r.Revision+3 {
		t.Fatalf("r=%+v e=%v", rr, e)
	}
	a, ok, _ := s.Get("a")
	if !ok || string(a.Value) != "4" || a.Revision != r.Revision+3 {
		t.Fatalf("a=%+v ok=%v", a, ok)
	}
	if snap := s.Snapshot(); snap.NextRevision != r.Revision+4 || snap.Keys != 1 || snap.ValueBytes != 1 {
		t.Fatalf("snap=%+v", snap)
	}
}

func TestRollbackKeepsRevisionAndGeneration(t *testing.T) {
	s := store(t)
	mustPut(t, s, "a", []byte("a"))
	before := s.Snapshot()
	// Semantic failure: delete of missing key after successful puts.
	_, e := s.Transact(Txn{Writes: []Write{put("b", []byte("b")), put("c", []byte("c")), del("zz")}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatalf("e=%v", e)
	}
	after := s.Snapshot()
	if after.NextRevision != before.NextRevision || after.Generation != before.Generation || after.Keys != before.Keys {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	// Capacity failure also rolls back fully.
	_, e = s.Transact(Txn{Writes: []Write{
		put("k1", []byte{}), put("k2", []byte{}), put("k3", []byte{}), put("k4", []byte{}),
		put("k5", []byte{}), put("k6", []byte{}), put("k7", []byte{}), put("k8", []byte{}),
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if snap := s.Snapshot(); snap.NextRevision != before.NextRevision || snap.Keys != before.Keys {
		t.Fatalf("snap=%+v", snap)
	}
}

func TestRevisionAllocationContiguous(t *testing.T) {
	s := store(t)
	var last uint64
	for i := 0; i < 5; i++ {
		r := mustPut(t, s, fmt.Sprintf("k%d", i), []byte{})
		if r.Revision != last+1 {
			t.Fatalf("r=%+v last=%d", r, last)
		}
		last = r.Revision
		if r.Generation != uint64(i+1) {
			t.Fatalf("gen=%d", r.Generation)
		}
	}
	// Read-only txn does not bump generation.
	r, e := s.Transact(Txn{Compares: []Compare{{Kind: Exists, Key: "k0"}}})
	if e != nil || !r.Succeeded || r.Generation != 5 || r.Revision != 5 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	if s.Snapshot().Generation != 5 {
		t.Fatal("generation bumped by read-only txn")
	}
}

func TestListPagingBoundaries(t *testing.T) {
	s, _ := New(Options{MaxKeys: 16, MaxValueBytes: 8, MaxNameBytes: 16})
	keys := []string{"a", "b", "c", "d", "e"}
	for _, k := range keys {
		mustPut(t, s, k, []byte(k))
	}
	// Page through with limit 2 using the after cursor.
	var got []string
	after := ""
	for {
		page, e := s.List("", after, 2)
		if e != nil {
			t.Fatal(e)
		}
		if len(page) == 0 {
			break
		}
		for _, en := range page {
			got = append(got, en.Key)
			after = en.Key
		}
		if len(got) > 10 {
			t.Fatal("paging did not terminate")
		}
	}
	if fmt.Sprint(got) != "[a b c d e]" {
		t.Fatalf("got=%v", got)
	}
	// After beyond the last key returns empty, not an error.
	v, e := s.List("", "z", 5)
	if e != nil || len(v) != 0 {
		t.Fatalf("v=%v e=%v", v, e)
	}
	// Prefix filtering with after cursor.
	v, e = s.List("", "b", 100)
	if e != nil || len(v) != 3 || v[0].Key != "c" {
		t.Fatalf("v=%v e=%v", v, e)
	}
	// Limit boundaries.
	if _, e = s.List("", "", 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e = s.List("", "", 1001); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e = s.List("", "", 1000); e != nil {
		t.Fatal(e)
	}
	// Empty prefix and after are valid.
	if v, e = s.List("", "", 1); e != nil || len(v) != 1 || v[0].Key != "a" {
		t.Fatalf("v=%v e=%v", v, e)
	}
}

func TestCapacityKeyCountAndBytes(t *testing.T) {
	// Total live bytes capacity is MaxKeys*MaxValueBytes = 4.
	s, _ := New(Options{MaxKeys: 2, MaxValueBytes: 2, MaxNameBytes: 8})
	if _, e := s.Transact(Txn{Writes: []Write{put("a", []byte("aa")), put("b", []byte("bb"))}}); e != nil {
		t.Fatal(e)
	}
	// Replacing within capacity succeeds.
	if _, e := s.Transact(Txn{Writes: []Write{put("a", []byte("cc"))}}); e != nil {
		t.Fatal(e)
	}
	// Third key exceeds key count.
	if _, e := s.Transact(Txn{Writes: []Write{put("c", []byte{})}}); !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	// Delete then add fits again.
	if _, e := s.Transact(Txn{Writes: []Write{del("b"), put("c", []byte("d"))}}); e != nil {
		t.Fatal(e)
	}
	if snap := s.Snapshot(); snap.Keys != 2 || snap.ValueBytes != 3 {
		t.Fatalf("snap=%+v", snap)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	s := store(t)
	in := []byte("orig")
	mustPut(t, s, "k", in)
	in[0] = 'X'
	e, _, _ := s.Get("k")
	if !bytes.Equal(e.Value, []byte("orig")) {
		t.Fatal("store aliases input")
	}
	// Mutating Get result must not affect the store.
	e.Value[0] = 'Y'
	e2, _, _ := s.Get("k")
	if !bytes.Equal(e2.Value, []byte("orig")) {
		t.Fatal("store aliases Get output")
	}
	// Mutating List and Snapshot results must not affect the store.
	l, _ := s.List("", "", 1)
	l[0].Value[0] = 'Z'
	snap := s.Snapshot()
	snap.Entries[0].Value[0] = 'W'
	e3, _, _ := s.Get("k")
	if !bytes.Equal(e3.Value, []byte("orig")) {
		t.Fatal("store aliases List/Snapshot output")
	}
	// Value compare input must not be retained.
	cv := []byte("orig")
	if _, err := s.Transact(Txn{Compares: []Compare{{Kind: Value, Key: "k", Value: cv}}}); err != nil {
		t.Fatal(err)
	}
	cv[0] = 'Q'
	e4, _, _ := s.Get("k")
	if !bytes.Equal(e4.Value, []byte("orig")) {
		t.Fatal("store mutated by compare input")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxKeys: 128, MaxValueBytes: 64, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				e, _, _ := s.Get(k)
				var cmp []Compare
				if e.Revision != 0 {
					cmp = append(cmp, Compare{Kind: Revision, Key: k, Revision: e.Revision})
				} else {
					cmp = append(cmp, Compare{Kind: NotExists, Key: k})
				}
				_, _ = s.Transact(Txn{Compares: cmp, Writes: []Write{put(k, []byte{byte(j)})}})
				_, _ = s.List("key-", "", 50)
				_ = s.Snapshot()
			}
		}()
	}
	wg.Wait()
	snap := s.Snapshot()
	if snap.Keys != 16 || snap.ValueBytes != 16 {
		t.Fatalf("snap=%+v", snap)
	}
	// Revisions are unique and contiguous from 1..NextRevision-1.
	seen := map[uint64]bool{}
	for _, en := range snap.Entries {
		if en.Revision == 0 || en.Revision >= snap.NextRevision || seen[en.Revision] {
			t.Fatalf("bad revision %d next=%d", en.Revision, snap.NextRevision)
		}
		seen[en.Revision] = true
	}
}

func TestGetValidationAndMissing(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad key"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, _, e := s.Get(""); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); e != nil || ok {
		t.Fatalf("ok=%v e=%v", ok, e)
	}
}

func TestNewOptionsValidation(t *testing.T) {
	for _, o := range []Options{
		{}, {MaxKeys: 1}, {MaxKeys: 1, MaxValueBytes: 1},
		{MaxKeys: -1, MaxValueBytes: 1, MaxNameBytes: 1},
		{MaxKeys: 1, MaxValueBytes: 0, MaxNameBytes: 1},
		{MaxKeys: 1, MaxValueBytes: 1, MaxNameBytes: 0},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("o=%+v e=%v", o, e)
		}
	}
}
