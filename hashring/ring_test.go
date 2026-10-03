package hashring

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
)

// mapHasher maps exact inputs to hashes so tests can place tokens precisely.
func mapHasher(m map[string]uint64) Hasher {
	return func(b []byte) uint64 {
		if h, ok := m[string(b)]; ok {
			return h
		}
		return 0
	}
}

func TestCollisionDeterministicOrder(t *testing.T) {
	// All tokens of "a" and "b" collide at hash 5; "c" sits at 9.
	h := mapHasher(map[string]uint64{
		"a#0": 5, "a#1": 5, "b#0": 5, "b#1": 5, "c#0": 9,
	})
	r, err := New(Options{MaxNodes: 4, MaxTokens: 8, MaxValueBytes: 8, Hasher: h})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyBatch([]Change{
		{Type: ChangeAdd, Node: Node{ID: "c", Weight: 1}},
		{Type: ChangeAdd, Node: Node{ID: "b", Weight: 2}},
		{Type: ChangeAdd, Node: Node{ID: "a", Weight: 2}},
	}); err != nil {
		t.Fatal(err)
	}
	want := []TokenView{
		{5, "a", 0}, {5, "a", 1}, {5, "b", 0}, {5, "b", 1}, {9, "c", 0},
	}
	if got := r.Snapshot().Tokens; !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens=%v want %v", got, want)
	}
	// No token is lost to collisions.
	if n := len(r.Snapshot().Tokens); n != 5 {
		t.Fatalf("token count=%d want 5", n)
	}
}

func TestLookupWraparound(t *testing.T) {
	// Tokens at hashes 10 and 20; key hashes to 30, so lookup wraps to 10.
	h := mapHasher(map[string]uint64{
		"a#0": 10, "b#0": 20, "key": 30,
	})
	r, err := New(Options{MaxNodes: 4, MaxTokens: 8, MaxValueBytes: 8, Hasher: h})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyBatch([]Change{
		{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1, Value: []byte("A")}},
		{Type: ChangeAdd, Node: Node{ID: "b", Weight: 1, Value: []byte("B")}},
	}); err != nil {
		t.Fatal(err)
	}
	owners, _, err := r.Lookup([]byte("key"), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(owners) != 2 || owners[0].ID != "a" || owners[1].ID != "b" {
		t.Fatalf("wraparound owners=%v", owners)
	}
	// Key exactly on a token starts at that token.
	owners, _, err = r.Lookup([]byte("a#0"), 1)
	if err != nil || len(owners) != 1 || owners[0].ID != "a" {
		t.Fatalf("exact-hit owners=%v err=%v", owners, err)
	}
	// Count larger than node count returns all distinct nodes once.
	owners, _, err = r.Lookup([]byte("key"), 4)
	if err != nil || len(owners) != 2 {
		t.Fatalf("clamped owners=%v err=%v", owners, err)
	}
}

func TestRollbackOnCapacityAndSemantics(t *testing.T) {
	r, err := New(Options{MaxNodes: 2, MaxTokens: 2, MaxValueBytes: 4, Hasher: testHasher})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Apply(Change{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1, Value: []byte("aa")}}); err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	cases := [][]Change{
		// token budget exceeded in final state
		{{Type: ChangeAdd, Node: Node{ID: "b", Weight: 2}}},
		// value budget exceeded in final state
		{{Type: ChangeAdd, Node: Node{ID: "b", Weight: 1, Value: []byte("bbb")}}},
		// node budget exceeded
		{{Type: ChangeAdd, Node: Node{ID: "b", Weight: 1}}, {Type: ChangeAdd, Node: Node{ID: "c", Weight: 1}}},
		// duplicate then valid op: whole batch fails
		{{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1}}, {Type: ChangeRemove, ID: "a"}},
	}
	for i, batch := range cases {
		if _, err := r.ApplyBatch(batch); err == nil {
			t.Fatalf("case %d unexpectedly succeeded", i)
		}
		if got := r.Snapshot(); !reflect.DeepEqual(before, got) {
			t.Fatalf("case %d mutated state: %+v", i, got)
		}
	}
	// Generation did not advance on failures; one success advances by one.
	gen, err := r.Apply(Change{Type: ChangeUpdate, ID: "a", Node: Node{Weight: 1, Value: []byte("z")}})
	if err != nil || gen != before.Generation+1 {
		t.Fatalf("gen=%d err=%v", gen, err)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	r := newTestRing(t)
	v := []byte("abc")
	if _, err := r.Apply(Change{Type: ChangeAdd, Node: Node{ID: "n", Weight: 1, Value: v}}); err != nil {
		t.Fatal(err)
	}
	v[0] = 'X' // mutating input must not affect the ring
	s1 := r.Snapshot()
	if string(s1.Nodes[0].Value) != "abc" {
		t.Fatalf("input alias: %q", s1.Nodes[0].Value)
	}
	// Mutating one snapshot must not affect later snapshots.
	s1.Nodes[0].Value[0] = 'Y'
	s2 := r.Snapshot()
	if string(s2.Nodes[0].Value) != "abc" {
		t.Fatalf("snapshot alias: %q", s2.Nodes[0].Value)
	}
	// Mutating lookup results must not affect the ring or other lookups.
	o1, _, err := r.Lookup([]byte("k"), 1)
	if err != nil {
		t.Fatal(err)
	}
	o1[0].Value[0] = 'Z'
	o2, _, err := r.Lookup([]byte("k"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(o2[0].Value) != "abc" || string(r.Snapshot().Nodes[0].Value) != "abc" {
		t.Fatalf("lookup alias: %q", o2[0].Value)
	}
}

func TestConcurrentAccess(t *testing.T) {
	r, err := New(Options{MaxNodes: 64, MaxTokens: 512, MaxValueBytes: 1 << 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Apply(Change{Type: ChangeAdd, Node: Node{ID: "seed", Weight: 1, Value: []byte("s")}}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			id := fmt.Sprintf("node-%d", w)
			for i := 0; i < 50; i++ {
				_, _ = r.Apply(Change{Type: ChangeAdd, Node: Node{ID: id, Weight: 1, Value: []byte{byte(w)}}})
				_, _ = r.Apply(Change{Type: ChangeUpdate, ID: id, Node: Node{Weight: 2, Value: []byte{byte(i)}}})
				_, _ = r.Apply(Change{Type: ChangeRemove, ID: id})
				_, _, _ = r.Lookup([]byte(fmt.Sprintf("key-%d-%d", w, i)), 3)
				_ = r.Snapshot()
			}
		}(w)
	}
	wg.Wait()
	s := r.Snapshot()
	if s.Generation == 0 || len(s.Nodes) == 0 {
		t.Fatalf("snapshot=%+v", s)
	}
	// Token ordering invariant still holds after concurrent churn.
	if !sort.SliceIsSorted(s.Tokens, func(i, j int) bool {
		a, b := s.Tokens[i], s.Tokens[j]
		if a.Hash != b.Hash {
			return a.Hash < b.Hash
		}
		if a.NodeID != b.NodeID {
			return a.NodeID < b.NodeID
		}
		return a.Replica < b.Replica
	}) {
		t.Fatal("tokens not sorted")
	}
}

func TestErrorsIsSentinels(t *testing.T) {
	r := newTestRing(t)
	if _, err := r.Apply(Change{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Apply(Change{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1}}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate=%v", err)
	}
	if _, err := r.Apply(Change{Type: ChangeRemove, ID: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("notfound=%v", err)
	}
}
