package jsondoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustStore(t *testing.T, doc string, max int) *Store {
	t.Helper()
	s, err := New([]byte(doc), Options{MaxNodes: max})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func op(name, path, value string) Operation {
	o := Operation{Op: name, Path: path}
	if value != "" {
		o.Value = json.RawMessage(value)
	}
	return o
}

func TestNewValidationAndSnapshot(t *testing.T) {
	if _, err := New([]byte(`{}`), Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("options=%v", err)
	}
	for _, s := range []string{"", `{} x`, `{"a":}`} {
		if _, err := New([]byte(s), Options{MaxNodes: 10}); !errors.Is(err, ErrInvalidJSON) {
			t.Fatalf("json %q: %v", s, err)
		}
	}
	if _, err := New([]byte(`{"a":[1,true,null]}`), Options{MaxNodes: 4}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity=%v", err)
	}
	s := mustStore(t, `{"b":2,"a":1}`, 10)
	snap := s.Snapshot()
	if snap.Revision != 1 || snap.Nodes != 3 || string(snap.Document) != `{"a":1,"b":2}` {
		t.Fatalf("snap=%+v", snap)
	}
	snap.Document[0] = 'X'
	if string(s.Snapshot().Document) != `{"a":1,"b":2}` {
		t.Fatal("snapshot aliases")
	}
}

func TestPointerEscapesAndObjectOperations(t *testing.T) {
	s := mustStore(t, `{"a/b":{"~key":1}}`, 20)
	r, err := s.Apply(1, []Operation{op("test", "/a~1b/~0key", "1.0"), op("add", "/a~1b/new", "2"), op("replace", "/a~1b/~0key", "3"), {Op: "remove", Path: "/a~1b/new"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Revision != 2 || string(r.Document) != `{"a/b":{"~key":3}}` {
		t.Fatalf("r=%+v", r)
	}
	for _, p := range []string{"x", "/~2", "/~"} {
		_, err := s.Apply(2, []Operation{op("test", p, "null")})
		if !errors.Is(err, ErrInvalidPointer) {
			t.Fatalf("%q: %v", p, err)
		}
	}
}

func TestArrayIndicesAndAppend(t *testing.T) {
	s := mustStore(t, `["a","c"]`, 20)
	r, err := s.Apply(1, []Operation{op("add", "/1", `"b"`), op("add", "/-", `"d"`), op("remove", "/0", ""), op("replace", "/1", `"C"`)})
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Document) != `["b","C","d"]` {
		t.Fatalf("doc=%s", r.Document)
	}
	for _, p := range []string{"/01", "/+1", "/-", "/9"} {
		_, err := s.Apply(2, []Operation{op("test", p, `"x"`)})
		if err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
}

func TestMoveCopyAndDescendant(t *testing.T) {
	s := mustStore(t, `{"a":{"x":1},"arr":["p","q","r"]}`, 30)
	r, err := s.Apply(1, []Operation{{Op: "copy", From: "/a", Path: "/b"}, {Op: "move", From: "/arr/0", Path: "/arr/2"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Document) != `{"a":{"x":1},"arr":["q","r","p"],"b":{"x":1}}` {
		t.Fatalf("doc=%s", r.Document)
	}
	_, err = s.Apply(2, []Operation{{Op: "move", From: "/a", Path: "/a/y"}})
	if !errors.Is(err, ErrMoveIntoChild) {
		t.Fatalf("descendant=%v", err)
	}
	r, err = s.Apply(2, []Operation{{Op: "move", From: "/a", Path: "/a"}})
	if err != nil || r.Revision != 3 {
		t.Fatalf("same move=%+v %v", r, err)
	}
}

func TestRootSemantics(t *testing.T) {
	s := mustStore(t, `{"a":1}`, 20)
	r, err := s.Apply(1, []Operation{op("add", "", `[1,2]`)})
	if err != nil || string(r.Document) != `[1,2]` {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if _, err := s.Apply(2, []Operation{{Op: "remove", Path: ""}}); !errors.Is(err, ErrRootRemoval) {
		t.Fatalf("remove root=%v", err)
	}
	r, err = s.Apply(2, []Operation{{Op: "move", From: "/0", Path: ""}})
	if err != nil || string(r.Document) != "1" {
		t.Fatalf("move root=%+v err=%v", r, err)
	}
}

func TestTestNumericSemanticEquality(t *testing.T) {
	s := mustStore(t, `{"n":1,"o":{"a":2,"b":3}}`, 20)
	r, err := s.Apply(1, []Operation{op("test", "/n", "1e0"), op("test", "/o", `{"b":3.0,"a":2e0}`)})
	if err != nil || r.Revision != 1 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	_, err = s.Apply(1, []Operation{op("test", "/n", "1.01")})
	if !errors.Is(err, ErrTestFailed) {
		t.Fatalf("test=%v", err)
	}
}

func TestValidationBeforeRevisionAndOpError(t *testing.T) {
	s := mustStore(t, `{}`, 10)
	_, err := s.Apply(99, []Operation{{Op: "unknown", Path: ""}})
	var oe *OpError
	if !errors.As(err, &oe) || oe.Index != 0 || !errors.Is(err, ErrInvalidOperation) {
		t.Fatalf("err=%#v", err)
	}
	_, err = s.Apply(99, nil)
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("revision=%v", err)
	}
	_, err = s.Apply(1, []Operation{{Op: "remove", Path: "/missing"}})
	if !errors.As(err, &oe) || !errors.Is(err, ErrNotFound) {
		t.Fatalf("exec=%#v", err)
	}
}

func TestAtomicRollbackAndFinalCapacity(t *testing.T) {
	s := mustStore(t, `{"a":1}`, 4)
	before := s.Snapshot()
	r, err := s.Apply(1, []Operation{op("add", "/large", `[1,2,3,4]`), {Op: "remove", Path: "/large"}, op("add", "/b", "2")})
	if err != nil || r.Nodes != 3 {
		t.Fatalf("temporary=%+v err=%v", r, err)
	}
	before = s.Snapshot()
	_, err = s.Apply(2, []Operation{op("add", "/x", `[1,2,3]`)})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity=%v", err)
	}
	after := s.Snapshot()
	if string(before.Document) != string(after.Document) || before.Revision != after.Revision {
		t.Fatalf("mutated")
	}
}

func TestInputOwnership(t *testing.T) {
	initial := []byte(`{"a":1}`)
	s, err := New(initial, Options{MaxNodes: 10})
	if err != nil {
		t.Fatal(err)
	}
	initial[2] = 'X'
	v := json.RawMessage(`{"x":2}`)
	r, err := s.Apply(1, []Operation{{Op: "add", Path: "/b", Value: v}})
	if err != nil {
		t.Fatal(err)
	}
	v[2] = 'Y'
	r.Document[0] = 'Z'
	if string(s.Snapshot().Document) != `{"a":1,"b":{"x":2}}` {
		t.Fatal("alias")
	}
}

func TestConcurrentRevisionCAS(t *testing.T) {
	s := mustStore(t, `{}`, 1000)
	var wg sync.WaitGroup
	success := 0
	var mu sync.Mutex
	for i := 0; i < 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Apply(1, []Operation{op("add", fmt.Sprintf("/k%d", i), "1")})
			if err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			} else if !errors.Is(err, ErrRevisionConflict) {
				t.Errorf("err=%v", err)
			}
		}()
	}
	wg.Wait()
	if success != 1 || s.Snapshot().Revision != 2 {
		t.Fatalf("success=%d snap=%+v", success, s.Snapshot())
	}
}
