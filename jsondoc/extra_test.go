package jsondoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestStructuralValidationErrors(t *testing.T) {
	s := mustStore(t, `{}`, 10)
	cases := []struct {
		op  Operation
		err error
	}{
		{Operation{Op: "ADD", Path: ""}, ErrInvalidOperation},
		{Operation{Op: "add", Path: "/a"}, ErrInvalidJSON},                            // missing value
		{Operation{Op: "add", Path: "/a", Value: json.RawMessage{}}, ErrInvalidJSON},  // empty non-nil
		{Operation{Op: "add", Path: "/a", Value: json.RawMessage(`1 2`)}, ErrInvalidJSON},
		{Operation{Op: "add", Path: "/a", From: "/b", Value: json.RawMessage(`1`)}, ErrInvalidOperation},
		{Operation{Op: "remove", Path: "/a", Value: json.RawMessage(`1`)}, ErrInvalidOperation},
		{Operation{Op: "move", From: "/a", Path: "/b", Value: json.RawMessage(`1`)}, ErrInvalidOperation},
		{Operation{Op: "copy", From: "x", Path: "/b"}, ErrInvalidPointer},
		{Operation{Op: "test", Path: "/a/~3", Value: json.RawMessage(`1`)}, ErrInvalidPointer},
	}
	for i, c := range cases {
		_, err := s.Apply(1, []Operation{c.op})
		var oe *OpError
		if !errors.As(err, &oe) || oe.Index != 0 || !errors.Is(err, c.err) {
			t.Fatalf("case %d: err=%#v want %v", i, err, c.err)
		}
	}
	// structural error in a later op reports its index
	_, err := s.Apply(1, []Operation{op("add", "/ok", "1"), {Op: "bogus", Path: "/x"}})
	var oe *OpError
	if !errors.As(err, &oe) || oe.Index != 1 || oe.Path != "/x" {
		t.Fatalf("index=%#v", oe)
	}
}

func TestPointerBoundaries(t *testing.T) {
	s := mustStore(t, `{"":{"":1},"a":[10,20]}`, 20)
	// empty object tokens are valid
	r, err := s.Apply(1, []Operation{op("test", "//", "1")})
	if err != nil {
		t.Fatal(err)
	}
	_ = r
	for _, p := range []string{"/a/-", "/a/00", "/a/ 1", "/a/1 ", "/a/99999999999999999999999999"} {
		if _, err := s.Apply(1, []Operation{op("test", p, "1")}); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	// traversal through scalar
	_, err = s.Apply(1, []Operation{op("test", "/a/0/x", "1")})
	if !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("type=%v", err)
	}
	// add at index == length appends
	r, err = s.Apply(1, []Operation{op("add", "/a/2", "30")})
	if err != nil || string(r.Document) != `{"":{"":1},"a":[10,20,30]}` {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	// add beyond length
	if _, err := s.Apply(2, []Operation{op("add", "/a/5", "1")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oob=%v", err)
	}
}

func TestMoveRootAndNestedArray(t *testing.T) {
	s := mustStore(t, `{"a":{"b":[1,2,3]}}`, 30)
	// moving root anywhere is moving into a descendant
	if _, err := s.Apply(1, []Operation{{Op: "move", From: "", Path: "/a/c"}}); !errors.Is(err, ErrMoveIntoChild) {
		t.Fatalf("root move=%v", err)
	}
	// move within nested array shifts elements
	r, err := s.Apply(1, []Operation{{Op: "move", From: "/a/b/2", Path: "/a/b/0"}})
	if err != nil || string(r.Document) != `{"a":{"b":[3,1,2]}}` {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	// move object member to root-level new key
	r, err = s.Apply(2, []Operation{{Op: "move", From: "/a/b", Path: "/b"}})
	if err != nil || string(r.Document) != `{"a":{},"b":[3,1,2]}` {
		t.Fatalf("r=%+v err=%v", r, err)
	}
}

func TestCopyIsDeepAndIsolated(t *testing.T) {
	s := mustStore(t, `{"a":{"x":[1]}}`, 30)
	_, err := s.Apply(1, []Operation{
		{Op: "copy", From: "/a", Path: "/b"},
		op("add", "/b/x/-", "2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(s.Snapshot().Document) != `{"a":{"x":[1]},"b":{"x":[1,2]}}` {
		t.Fatalf("doc=%s", s.Snapshot().Document)
	}
}

func TestReplaceRootAndMissing(t *testing.T) {
	s := mustStore(t, `1`, 10)
	r, err := s.Apply(1, []Operation{op("replace", "", `{"k":"v"}`)})
	if err != nil || string(r.Document) != `{"k":"v"}` {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if _, err := s.Apply(2, []Operation{op("replace", "/missing", "1")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replace=%v", err)
	}
	if _, err := s.Apply(2, []Operation{op("add", "/k/x", "1")}); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("add into scalar=%v", err)
	}
}

func TestNumberLexemesPreservedAndCompared(t *testing.T) {
	s := mustStore(t, `{"big":9007199254740993,"e":1.50e2}`, 10)
	snap := s.Snapshot()
	if string(snap.Document) != `{"big":9007199254740993,"e":1.50e2}` {
		t.Fatalf("lexemes=%s", snap.Document)
	}
	// exact big-integer semantics: 9007199254740993 != 9007199254740994 even though float64 collides
	if _, err := s.Apply(1, []Operation{op("test", "/big", "9007199254740994")}); !errors.Is(err, ErrTestFailed) {
		t.Fatalf("big=%v", err)
	}
	if _, err := s.Apply(1, []Operation{op("test", "/e", "150")}); err != nil {
		t.Fatalf("exp=%v", err)
	}
}

func TestEmptyAndTestOnlyBatchKeepsRevision(t *testing.T) {
	s := mustStore(t, `{"a":1}`, 10)
	r, err := s.Apply(1, nil)
	if err != nil || r.Revision != 1 || string(r.Document) != `{"a":1}` {
		t.Fatalf("empty=%+v err=%v", r, err)
	}
	r, err = s.Apply(1, []Operation{op("test", "/a", "1"), op("test", "/a", "1.0")})
	if err != nil || r.Revision != 1 {
		t.Fatalf("testonly=%+v err=%v", r, err)
	}
}

func TestFailedBatchLeavesNoTrace(t *testing.T) {
	s := mustStore(t, `{"a":[1,2]}`, 10)
	_, err := s.Apply(1, []Operation{
		op("add", "/a/-", "3"),
		{Op: "remove", Path: "/nope"},
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	snap := s.Snapshot()
	if snap.Revision != 1 || string(snap.Document) != `{"a":[1,2]}` {
		t.Fatalf("snap=%+v", snap)
	}
}

func TestConcurrentMixedAccess(t *testing.T) {
	s := mustStore(t, `{"n":0}`, 10000)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				for {
					_, err := s.Apply(s.Snapshot().Revision, []Operation{
						op("add", fmt.Sprintf("/g%d-%d", g, i), "1"),
						op("test", "/n", "0"),
					})
					if err == nil {
						break
					}
					if !errors.Is(err, ErrRevisionConflict) {
						t.Errorf("err=%v", err)
						break
					}
				}
			}
		}()
	}
	wg.Wait()
	snap := s.Snapshot()
	if snap.Nodes != 8*50+2 {
		t.Fatalf("nodes=%d doc=%.100s", snap.Nodes, snap.Document)
	}
}

func TestSnapshotIsolationAcrossCalls(t *testing.T) {
	s := mustStore(t, `{"a":1}`, 10)
	s1 := s.Snapshot()
	r, _ := s.Apply(1, []Operation{op("add", "/b", "2")})
	s2 := s.Snapshot()
	if strings.Contains(string(s1.Document), `"b"`) || !strings.Contains(string(s2.Document), `"b"`) {
		t.Fatal("snapshots not isolated")
	}
	if string(r.Document) != string(s2.Document) {
		t.Fatal("result/snapshot mismatch")
	}
}
