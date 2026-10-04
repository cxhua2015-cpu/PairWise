package casstore

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func opts() Options { return Options{MaxKeys: 8, MaxValueBytes: 32, MaxNameBytes: 16} }
func store(t *testing.T) *Store {
	t.Helper()
	s, e := New(opts())
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func put(k string, v []byte) Write { return Write{Kind: Put, Key: k, Value: v} }
func del(k string) Write           { return Write{Kind: Delete, Key: k} }
func TestValidationBeforeState(t *testing.T) {
	if _, e := New(Options{}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	s := store(t)
	_, _ = s.Transact(Txn{Writes: []Write{put("x", []byte("x"))}})
	before := s.Snapshot()
	_, e := s.Transact(Txn{Compares: []Compare{{Kind: Revision, Key: "x", Revision: 99}, {Kind: 99, Key: "x"}}, Writes: []Write{put("y", []byte{})}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatalf("e=%v", e)
	}
}
func TestCompareSnapshotAndSequentialWrites(t *testing.T) {
	s := store(t)
	r, e := s.Transact(Txn{Compares: []Compare{{Kind: NotExists, Key: "x"}}, Writes: []Write{put("x", []byte("a")), put("x", []byte("b"))}})
	if e != nil || !r.Succeeded || r.Revision != 2 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	x, ok, _ := s.Get("x")
	if !ok || x.Revision != 2 || string(x.Value) != "b" {
		t.Fatalf("x=%+v", x)
	}
	r, e = s.Transact(Txn{Compares: []Compare{{Kind: NotExists, Key: "x"}}, Writes: []Write{del("missing")}})
	if e != nil || r.Succeeded || s.Snapshot().NextRevision != 3 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
}
func TestRollbackRevisionAndFinalCapacity(t *testing.T) {
	s, _ := New(Options{MaxKeys: 1, MaxValueBytes: 2, MaxNameBytes: 8})
	_, _ = s.Transact(Txn{Writes: []Write{put("a", []byte("aa"))}})
	before := s.Snapshot()
	_, e := s.Transact(Txn{Writes: []Write{del("a"), put("b", []byte("bb")), del("missing")}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatalf("e=%v", e)
	}
	r, e := s.Transact(Txn{Writes: []Write{put("b", []byte("bb")), del("a")}})
	if e != nil || r.Revision != 2 || s.Snapshot().Keys != 1 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
}
func TestComparesAndOwnership(t *testing.T) {
	s := store(t)
	in := []byte("abc")
	r, _ := s.Transact(Txn{Writes: []Write{put("x", in)}})
	in[0] = 'X'
	x, _, _ := s.Get("x")
	if string(x.Value) != "abc" {
		t.Fatal("input alias")
	}
	checks := []Compare{{Kind: Exists, Key: "x"}, {Kind: Revision, Key: "x", Revision: r.Revision}, {Kind: Value, Key: "x", Value: []byte("abc")}, {Kind: NotExists, Key: "y"}}
	rr, e := s.Transact(Txn{Compares: checks})
	if e != nil || !rr.Succeeded || rr.Generation != 1 {
		t.Fatalf("r=%+v e=%v", rr, e)
	}
	x.Value[0] = 'Y'
	y, _, _ := s.Get("x")
	if !bytes.Equal(y.Value, []byte("abc")) {
		t.Fatal("output alias")
	}
}
func TestListPagingAndSnapshot(t *testing.T) {
	s := store(t)
	_, _ = s.Transact(Txn{Writes: []Write{put("cfg/c", []byte{}), put("cfg/a", []byte{}), put("other", []byte{}), put("cfg/b", []byte{})}})
	v, e := s.List("cfg/", "cfg/a", 1)
	if e != nil || len(v) != 1 || v[0].Key != "cfg/b" {
		t.Fatalf("v=%v e=%v", v, e)
	}
	snap := s.Snapshot()
	if snap.Keys != 4 || snap.NextRevision != 5 || snap.Entries[0].Key != "cfg/a" {
		t.Fatalf("s=%+v", snap)
	}
	if _, e = s.List("bad?", "", 1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}
func TestConcurrentTransactionsAndReads(t *testing.T) {
	s, _ := New(Options{MaxKeys: 64, MaxValueBytes: 256, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("k-%d", i)
			if _, e := s.Transact(Txn{Writes: []Write{put(k, []byte{k[0]})}}); e != nil {
				t.Error(e)
			}
			_, _, _ = s.Get(k)
			_, _ = s.List("k-", "", 100)
			_ = s.Snapshot()
		}()
	}
	wg.Wait()
	if x := s.Snapshot(); x.Keys != 32 || x.ValueBytes != 32 || x.NextRevision != 33 {
		t.Fatalf("s=%+v", x)
	}
}
