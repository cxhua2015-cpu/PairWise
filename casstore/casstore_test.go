package casstore

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestCompareFailureNoStateChange(t *testing.T) {
	s := store(t)
	_, _ = s.Transact(Txn{Writes: []Write{put("a", []byte("1"))}})
	before := s.Snapshot()
	r, e := s.Transact(Txn{
		Compares: []Compare{{Kind: Value, Key: "a", Value: []byte("wrong")}},
		Writes:   []Write{put("b", []byte("2"))},
	})
	if e != nil || r.Succeeded {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	if got := s.Snapshot(); got.NextRevision != before.NextRevision || got.Generation != before.Generation || got.Keys != before.Keys {
		t.Fatalf("state changed: %+v -> %+v", before, got)
	}
	if _, ok, _ := s.Get("b"); ok {
		t.Fatal("b should not exist")
	}
}

func TestDeleteRecreateAndRevisionOrdering(t *testing.T) {
	s := store(t)
	r, e := s.Transact(Txn{Writes: []Write{put("k", []byte("v1")), del("k"), put("k", []byte("v2"))}})
	if e != nil || !r.Succeeded || r.Revision != 2 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	ent, ok, _ := s.Get("k")
	if !ok || ent.Revision != 2 || string(ent.Value) != "v2" {
		t.Fatalf("ent=%+v", ent)
	}
	r, e = s.Transact(Txn{Compares: []Compare{{Kind: Revision, Key: "k", Revision: 2}}})
	if e != nil || !r.Succeeded {
		t.Fatalf("r=%+v e=%v", r, e)
	}
}

func TestKeyValidation(t *testing.T) {
	s := store(t)
	for _, k := range []string{"", "bad key", "bad?", "toolongkeytoolongkey", "汉字"} {
		if _, e := s.Transact(Txn{Writes: []Write{put(k, []byte("v"))}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: e=%v", k, e)
		}
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestListBoundaries(t *testing.T) {
	s := store(t)
	_, _ = s.Transact(Txn{Writes: []Write{put("a", []byte{}), put("b", []byte{}), put("c", []byte{})}})
	for _, limit := range []int{0, -1, 1001} {
		if _, e := s.List("", "", limit); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("limit %d: e=%v", limit, e)
		}
	}
	v, e := s.List("", "", 1000)
	if e != nil || len(v) != 3 || v[0].Key != "a" || v[2].Key != "c" {
		t.Fatalf("v=%v e=%v", v, e)
	}
	v, _ = s.List("", "c", 10)
	if len(v) != 0 {
		t.Fatalf("v=%v", v)
	}
	v, _ = s.List("z", "", 10)
	if len(v) != 0 {
		t.Fatalf("v=%v", v)
	}
	v, _ = s.List("", "a", 1)
	if len(v) != 1 || v[0].Key != "b" {
		t.Fatalf("v=%v", v)
	}
}

func TestCapacityTotalBytes(t *testing.T) {
	s, e := New(Options{MaxKeys: 10, MaxValueBytes: 4, MaxNameBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Transact(Txn{Writes: []Write{put("a", []byte("aa")), put("b", []byte("bb"))}}); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	_, e = s.Transact(Txn{Writes: []Write{put("c", []byte("c"))}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if got := s.Snapshot(); got.Keys != before.Keys || got.ValueBytes != before.ValueBytes || got.NextRevision != before.NextRevision {
		t.Fatalf("state changed: %+v -> %+v", before, got)
	}
	// value exceeding per-value limit is invalid input
	if _, e = s.Transact(Txn{Writes: []Write{put("c", []byte("12345"))}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
}

func TestListOwnershipIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Transact(Txn{Writes: []Write{put("a", []byte("x"))}})
	v, _ := s.List("", "", 10)
	v[0].Value[0] = 'Z'
	ent, _, _ := s.Get("a")
	if string(ent.Value) != "x" {
		t.Fatal("list output aliases store")
	}
	snap := s.Snapshot()
	snap.Entries[0].Value[0] = 'Z'
	ent, _, _ = s.Get("a")
	if string(ent.Value) != "x" {
		t.Fatal("snapshot output aliases store")
	}
}

func TestConcurrentCompareAndSwap(t *testing.T) {
	s, _ := New(Options{MaxKeys: 64, MaxValueBytes: 4096, MaxNameBytes: 16})
	_, _ = s.Transact(Txn{Writes: []Write{put("n", []byte("0"))}})
	var wg sync.WaitGroup
	wins := make(chan uint64, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				cur, _, _ := s.Get("n")
				r, e := s.Transact(Txn{
					Compares: []Compare{{Kind: Revision, Key: "n", Revision: cur.Revision}},
					Writes:   []Write{put("n", []byte("x"))},
				})
				if e != nil {
					t.Error(e)
					return
				}
				if r.Succeeded {
					wins <- r.Revision
					return
				}
			}
		}()
	}
	wg.Wait()
	close(wins)
	seen := map[uint64]bool{}
	for rev := range wins {
		if seen[rev] {
			t.Fatalf("duplicate revision %d", rev)
		}
		seen[rev] = true
	}
	if len(seen) != 64 {
		t.Fatalf("wins=%d", len(seen))
	}
	if g := s.Snapshot().Generation; g != 65 {
		t.Fatalf("generation=%d", g)
	}
}

func TestReadOnlyTxnNoGenerationBump(t *testing.T) {
	s := store(t)
	r, _ := s.Transact(Txn{Compares: []Compare{{Kind: NotExists, Key: "x"}}})
	if !r.Succeeded || r.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatalf("r=%+v", r)
	}
	_, _ = s.Transact(Txn{Writes: []Write{put("x", []byte("v"))}})
	r, _ = s.Transact(Txn{Compares: []Compare{{Kind: Exists, Key: "x"}}})
	if !r.Succeeded || r.Generation != 1 || s.Snapshot().Generation != 1 {
		t.Fatalf("r=%+v", r)
	}
}

func TestConcurrentMixedLoad(t *testing.T) {
	s, _ := New(Options{MaxKeys: 128, MaxValueBytes: 8192, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("key/%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = s.Transact(Txn{Writes: []Write{put(k, []byte{byte(j)})}})
				_, _, _ = s.Get(k)
				_, _ = s.List("key/", "", 100)
				_ = s.Snapshot()
			}
		}()
	}
	wg.Wait()
	snap := s.Snapshot()
	if snap.Keys != 16 || snap.ValueBytes != 16 {
		t.Fatalf("snap=%+v", snap)
	}
}
