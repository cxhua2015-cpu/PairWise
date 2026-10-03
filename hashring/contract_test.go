package hashring

import (
	"errors"
	"reflect"
	"testing"
)

func testHasher(b []byte) uint64 {
	var n uint64
	for _, c := range b {
		n = n*131 + uint64(c)
	}
	return n % 17
}

func newTestRing(t *testing.T) *Ring {
	t.Helper()
	r, err := New(Options{MaxNodes: 8, MaxTokens: 32, MaxValueBytes: 64, Hasher: testHasher})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestOptionsAndValidation(t *testing.T) {
	bad := []Options{
		{},
		{MaxNodes: 0, MaxTokens: 1, MaxValueBytes: 1},
		{MaxNodes: 1, MaxTokens: 0, MaxValueBytes: 1},
		{MaxNodes: 1, MaxTokens: 1, MaxValueBytes: 0},
		{MaxNodes: 10001, MaxTokens: 1, MaxValueBytes: 1},
		{MaxNodes: 1, MaxTokens: 1000001, MaxValueBytes: 1},
		{MaxNodes: 1, MaxTokens: 1, MaxValueBytes: 64<<20 + 1},
	}
	for _, opts := range bad {
		if _, err := New(opts); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("New(%+v) error=%v", opts, err)
		}
	}
	r := newTestRing(t)
	for _, tc := range []struct {
		change Change
		want   error
	}{
		{Change{Type: 99}, ErrInvalidChange},
		{Change{Type: ChangeAdd, ID: "x", Node: Node{ID: "a", Weight: 1}}, ErrInvalidChange},
		{Change{Type: ChangeRemove, ID: "a", Node: Node{ID: "x"}}, ErrInvalidChange},
		{Change{Type: ChangeUpdate, ID: "a", Node: Node{ID: "x", Weight: 1}}, ErrInvalidChange},
		{Change{Type: ChangeAdd, Node: Node{ID: "bad/id", Weight: 1}}, ErrInvalidID},
		{Change{Type: ChangeAdd, Node: Node{ID: "a", Weight: 0}}, ErrInvalidWeight},
		{Change{Type: ChangeAdd, Node: Node{ID: "a", Weight: 129}}, ErrInvalidWeight},
		{Change{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1, Value: make([]byte, 1<<20+1)}}, ErrValueTooLarge},
	} {
		if _, err := r.Apply(tc.change); !errors.Is(err, tc.want) {
			t.Errorf("Apply(%+v) error=%v want %v", tc.change, err, tc.want)
		}
	}
}

func TestBatchAtomicityOrderAndGeneration(t *testing.T) {
	r := newTestRing(t)
	if gen, err := r.ApplyBatch(nil); err != nil || gen != 0 {
		t.Fatalf("empty batch=(%d,%v)", gen, err)
	}
	v := []byte("one")
	gen, err := r.ApplyBatch([]Change{
		{Type: ChangeAdd, Node: Node{ID: "a", Weight: 2, Value: v}},
		{Type: ChangeAdd, Node: Node{ID: "b", Weight: 1, Value: []byte("two")}},
	})
	if err != nil || gen != 1 {
		t.Fatalf("add batch=(%d,%v)", gen, err)
	}
	v[0] = 'X'
	if got := string(r.Snapshot().Nodes[0].Value); got != "one" {
		t.Fatalf("input alias: %q", got)
	}
	before := r.Snapshot()
	_, err = r.ApplyBatch([]Change{
		{Type: ChangeRemove, ID: "a"},
		{Type: ChangeAdd, Node: Node{ID: "c", Weight: 1}},
		{Type: ChangeUpdate, ID: "missing", Node: Node{Weight: 1}},
	})
	if !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("failed batch changed state: err=%v", err)
	}
	gen, err = r.ApplyBatch([]Change{
		{Type: ChangeRemove, ID: "a"},
		{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1, Value: []byte("new")}},
		{Type: ChangeUpdate, ID: "b", Node: Node{Weight: 2, Value: []byte("B")}},
	})
	if err != nil || gen != 2 {
		t.Fatalf("replace batch=(%d,%v)", gen, err)
	}
	got := r.Snapshot()
	if got.Generation != 2 || got.UsedValueBytes != 4 || len(got.Tokens) != 3 {
		t.Fatalf("snapshot=%+v", got)
	}
}

func TestStructuralErrorsPrecedeSemanticErrors(t *testing.T) {
	r := newTestRing(t)
	_, _ = r.Apply(Change{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1}})
	_, err := r.ApplyBatch([]Change{
		{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1}},
		{Type: ChangeAdd, Node: Node{ID: "bad/id", Weight: 1}},
	})
	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("error=%v, want structural invalid id", err)
	}
}

func TestCapacityUsesFinalState(t *testing.T) {
	r, err := New(Options{MaxNodes: 2, MaxTokens: 3, MaxValueBytes: 4, Hasher: testHasher})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.ApplyBatch([]Change{
		{Type: ChangeAdd, Node: Node{ID: "a", Weight: 2, Value: []byte("aa")}},
		{Type: ChangeAdd, Node: Node{ID: "b", Weight: 1, Value: []byte("bb")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ApplyBatch([]Change{
		{Type: ChangeRemove, ID: "a"},
		{Type: ChangeAdd, Node: Node{ID: "c", Weight: 2, Value: []byte("cc")}},
	}); err != nil {
		t.Fatalf("final-state replacement: %v", err)
	}
	before := r.Snapshot()
	if _, err = r.Apply(Change{Type: ChangeUpdate, ID: "b", Node: Node{Weight: 2, Value: []byte("bbb")}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity error=%v", err)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatal("capacity failure changed state")
	}
}

func TestTokenCollisionsOrderingAndLookup(t *testing.T) {
	constant := func([]byte) uint64 { return 7 }
	r, err := New(Options{MaxNodes: 4, MaxTokens: 8, MaxValueBytes: 20, Hasher: constant})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.ApplyBatch([]Change{
		{Type: ChangeAdd, Node: Node{ID: "b", Weight: 2, Value: []byte("B")}},
		{Type: ChangeAdd, Node: Node{ID: "a", Weight: 2, Value: []byte("A")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	wantTokens := []TokenView{{7, "a", 0}, {7, "a", 1}, {7, "b", 0}, {7, "b", 1}}
	if !reflect.DeepEqual(s.Tokens, wantTokens) {
		t.Fatalf("tokens=%v", s.Tokens)
	}
	owners, gen, err := r.Lookup([]byte("key"), 4)
	if err != nil || gen != 1 || len(owners) != 2 || owners[0].ID != "a" || owners[1].ID != "b" {
		t.Fatalf("Lookup=(%v,%d,%v)", owners, gen, err)
	}
	owners[0].Value[0] = 'X'
	if string(r.Snapshot().Nodes[0].Value) != "A" {
		t.Fatal("lookup value alias")
	}
}

func TestLookupValidationEmptyAndSnapshotIsolation(t *testing.T) {
	r := newTestRing(t)
	for _, tc := range []struct {
		key   []byte
		count int
	}{{nil, 1}, {[]byte("x"), 0}, {[]byte("x"), 9}} {
		if _, _, err := r.Lookup(tc.key, tc.count); !errors.Is(err, ErrInvalidLookup) {
			t.Fatalf("Lookup(%q,%d)=%v", tc.key, tc.count, err)
		}
	}
	if _, _, err := r.Lookup([]byte("x"), 1); !errors.Is(err, ErrEmpty) {
		t.Fatalf("empty=%v", err)
	}
	_, _ = r.Apply(Change{Type: ChangeAdd, Node: Node{ID: "a", Weight: 1, Value: []byte("v")}})
	s := r.Snapshot()
	s.Nodes[0].Value[0] = 'X'
	s.Nodes[0].ID = "changed"
	s.Tokens[0].NodeID = "changed"
	s2 := r.Snapshot()
	if s2.Nodes[0].ID != "a" || string(s2.Nodes[0].Value) != "v" || s2.Tokens[0].NodeID != "a" {
		t.Fatalf("snapshot aliases state: %+v", s2)
	}
}
