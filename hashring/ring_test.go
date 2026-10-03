package hashring

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

// posHasher maps token inputs "ID#r" to a fixed position table and hashes
// arbitrary keys by a simple sum, letting tests control ring layout exactly.
type posHasher struct {
	positions map[string]uint64
	keyHash   uint64
}

func (p posHasher) hash(b []byte) uint64 {
	if h, ok := p.positions[string(b)]; ok {
		return h
	}
	return p.keyHash
}

func TestCollisionDeterministicOrder(t *testing.T) {
	constant := func([]byte) uint64 { return 42 }
	r, err := New(Options{MaxNodes: 4, MaxTokens: 16, MaxValueBytes: 64, Hasher: constant})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.ApplyBatch([]Change{
		{Type: ChangeAdd, Node: Node{ID: "c", Weight: 1}},
		{Type: ChangeAdd, Node: Node{ID: "a", Weight: 3}},
		{Type: ChangeAdd, Node: Node{ID: "b", Weight: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []TokenView{
		{42, "a", 0}, {42, "a", 1}, {42, "a", 2},
		{42, "b", 0}, {42, "b", 1},
		{42, "c", 0},
	}
	if got := r.Snapshot().Tokens; !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens=%v want %v", got, want)
	}
	owners, _, err := r.Lookup([]byte("k"), 3)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{owners[0].ID, owners[1].ID, owners[2].ID}
	if !reflect.DeepEqual(ids, []string{"a", "b", "c"}) {
		t.Fatalf("owner order=%v", ids)
	}
}

func TestLookupWraparound(t *testing.T) {
	h := posHasher{positions: map[string]uint64{
		"a#0": 10, "b#0": 20, "c#0": 30,
	}, keyHash: 25}
	r, err := New(Options{MaxNodes: 4, MaxTokens: 8, MaxValueBytes: 16, Hasher: h.hash})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, err := r.Apply(Change{Type: ChangeAdd, Node: Node{ID: id, Weight: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	owners, _, err := r.Lookup([]byte("key"), 3)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{owners[0].ID, owners[1].ID, owners[2].ID}
	if !reflect.DeepEqual(got, []string{"c", "a", "b"}) {
		t.Fatalf("wraparound order=%v", got)
	}
	// count larger than node count returns all nodes.
	owners, _, err = r.Lookup([]byte("key"), 4)
	if err != nil || len(owners) != 3 {
		t.Fatalf("oversized count=(%v,%v)", owners, err)
	}
	// key hashing beyond the last token starts at the first token.
	h2 := posHasher{positions: map[string]uint64{"a#0": 10, "b#0": 20}, keyHash: 99}
	r2, _ := New(Options{MaxNodes: 4, MaxTokens: 8, MaxValueBytes: 16, Hasher: h2.hash})
	_, _ = r2.ApplyBatch([]Change{
		{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1}},
		{Type: ChangeAdd, Node: Node{ID: "b", Weight: 1}},
	})
	owners, _, err = r2.Lookup([]byte("k"), 1)
	if err != nil || owners[0].ID != "a" {
		t.Fatalf("past-end wrap=(%v,%v)", owners, err)
	}
}

func TestRollbackKeepsTokensAndGeneration(t *testing.T) {
	r, err := New(Options{MaxNodes: 2, MaxTokens: 2, MaxValueBytes: 8, Hasher: testHasher})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Apply(Change{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1, Value: []byte("v")}}); err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	failures := [][]Change{
		{{Type: ChangeAdd, Node: Node{ID: "b", Weight: 2}}},                    // token budget
		{{Type: ChangeAdd, Node: Node{ID: "b", Weight: 1, Value: []byte("123456789")}}}, // value budget
		{{Type: ChangeAdd, Node: Node{ID: "b", Weight: 1}},
			{Type: ChangeAdd, Node: Node{ID: "c", Weight: 1}}}, // node budget
		{{Type: ChangeRemove, ID: "a"}, {Type: ChangeRemove, ID: "a"}}, // semantic
	}
	for i, batch := range failures {
		if _, err := r.ApplyBatch(batch); err == nil {
			t.Fatalf("case %d unexpectedly succeeded", i)
		}
		if got := r.Snapshot(); !reflect.DeepEqual(before, got) {
			t.Fatalf("case %d mutated state: %+v", i, got)
		}
	}
	if got := r.Snapshot().Generation; got != 1 {
		t.Fatalf("generation advanced on failure: %d", got)
	}
	if !errors.Is(func() error { _, e := r.Apply(Change{Type: ChangeAdd, Node: Node{ID: "b", Weight: 2}}); return e }(), ErrCapacity) {
		t.Fatal("expected ErrCapacity")
	}
}

func TestOwnershipIsolation(t *testing.T) {
	r := newTestRing(t)
	in := []byte("in")
	if _, err := r.Apply(Change{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1, Value: in}}); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X'
	upd := []byte("up")
	if _, err := r.Apply(Change{Type: ChangeUpdate, ID: "a", Node: Node{Weight: 1, Value: upd}}); err != nil {
		t.Fatal(err)
	}
	upd[0] = 'Y'
	if got := string(r.Snapshot().Nodes[0].Value); got != "up" {
		t.Fatalf("input alias: %q", got)
	}
	owners, _, err := r.Lookup([]byte("k"), 1)
	if err != nil {
		t.Fatal(err)
	}
	owners[0].Value[0] = 'Z'
	s1 := r.Snapshot()
	s1.Nodes[0].Value[0] = 'W'
	if got := string(r.Snapshot().Nodes[0].Value); got != "up" {
		t.Fatalf("output alias: %q", got)
	}
}

func TestConcurrentAccess(t *testing.T) {
	r, err := New(Options{MaxNodes: 64, MaxTokens: 512, MaxValueBytes: 1 << 16})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			id := fmt.Sprintf("node-%d", w)
			for i := 0; i < 50; i++ {
				_, _ = r.Apply(Change{Type: ChangeAdd, Node: Node{ID: id, Weight: 1, Value: []byte("v")}})
				_, _, _ = r.Lookup([]byte(fmt.Sprintf("key-%d-%d", w, i)), 2)
				_ = r.Snapshot()
				_, _ = r.Apply(Change{Type: ChangeUpdate, ID: id, Node: Node{Weight: 2, Value: []byte("w")}})
				_, _ = r.Apply(Change{Type: ChangeRemove, ID: id})
			}
		}(w)
	}
	wg.Wait()
	s := r.Snapshot()
	if len(s.Nodes) > 64 || len(s.Tokens) > 512 {
		t.Fatalf("capacity violated: %+v", s)
	}
}

func TestDefaultHasherTokenInput(t *testing.T) {
	r, err := New(Options{MaxNodes: 2, MaxTokens: 4, MaxValueBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Apply(Change{Type: ChangeAdd, Node: Node{ID: "n1", Weight: 2}}); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	if len(s.Tokens) != 2 || s.Tokens[0].Hash > s.Tokens[1].Hash {
		t.Fatalf("tokens not hash-sorted: %v", s.Tokens)
	}
	// SHA-256 first 8 bytes big-endian of "n1#0" and "n1#1" must match.
	byReplica := map[int]uint64{}
	for _, tv := range s.Tokens {
		byReplica[tv.Replica] = tv.Hash
	}
	for replica, input := range []string{"n1#0", "n1#1"} {
		if got, want := byReplica[replica], defaultHasher([]byte(input)); got != want {
			t.Fatalf("replica %d hash=%d want %d", replica, got, want)
		}
	}
}
