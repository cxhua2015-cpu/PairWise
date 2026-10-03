package jsondoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestStructuralValidationDetails(t *testing.T) {
	s := mustStore(t, `{}`, 10)
	cases := []struct {
		name string
		op   Operation
		want error
	}{
		{"add empty value", Operation{Op: "add", Path: "/a", Value: json.RawMessage{}}, ErrInvalidJSON},
		{"add trailing", op("add", "/a", "1 2"), ErrInvalidJSON},
		{"add with from", Operation{Op: "add", Path: "/a", From: "/b", Value: json.RawMessage("1")}, ErrInvalidOperation},
		{"remove with value", Operation{Op: "remove", Path: "/a", Value: json.RawMessage("1")}, ErrInvalidOperation},
		{"remove with from", Operation{Op: "remove", Path: "/a", From: "/b"}, ErrInvalidOperation},
		{"move with value", Operation{Op: "move", Path: "/a", From: "/b", Value: json.RawMessage("1")}, ErrInvalidOperation},
		{"move bad from", Operation{Op: "move", Path: "/a", From: "x"}, ErrInvalidPointer},
		{"copy root from", Operation{Op: "copy", Path: "/a", From: ""}, nil},
		{"uppercase op", op("Add", "/a", "1"), ErrInvalidOperation},
		{"bad path", op("add", "a", "1"), ErrInvalidPointer},
	}
	for _, tc := range cases {
		_, err := s.Apply(1, []Operation{tc.op})
		if tc.want == nil {
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			continue
		}
		var oe *OpError
		if !errors.As(err, &oe) || oe.Index != 0 || oe.Path != tc.op.Path || !errors.Is(err, tc.want) {
			t.Fatalf("%s: %#v", tc.name, err)
		}
	}
	if s.Snapshot().Revision != 2 { // only "copy root from" mutated
		t.Fatalf("revision=%d", s.Snapshot().Revision)
	}
}

func TestOpErrorMessageAndIndex(t *testing.T) {
	s := mustStore(t, `{"a":1}`, 10)
	_, err := s.Apply(1, []Operation{
		op("add", "/b", "2"),
		{Op: "remove", Path: "/missing"},
	})
	var oe *OpError
	if !errors.As(err, &oe) || oe.Index != 1 {
		t.Fatalf("err=%#v", err)
	}
	if !strings.Contains(oe.Error(), "1") || !strings.Contains(oe.Error(), ErrNotFound.Error()) {
		t.Fatalf("msg=%q", oe.Error())
	}
	// rolled back: /b was never added
	if string(s.Snapshot().Document) != `{"a":1}` {
		t.Fatalf("doc=%s", s.Snapshot().Document)
	}
}

func TestPointerBoundaryCases(t *testing.T) {
	s := mustStore(t, `{"":{"":1},"arr":[10,20]}`, 20)
	// empty object token
	r, err := s.Apply(1, []Operation{op("test", "//", "1")})
	if err != nil || r.Revision != 1 {
		t.Fatalf("empty token: %+v %v", r, err)
	}
	// "-" only valid as final add token
	if _, err := s.Apply(1, []Operation{op("add", "/arr/-/x", "1")}); !errors.Is(err, ErrInvalidIndex) {
		t.Fatalf("mid dash: %v", err)
	}
	if _, err := s.Apply(1, []Operation{{Op: "remove", Path: "/arr/-"}}); !errors.Is(err, ErrInvalidIndex) {
		t.Fatalf("remove dash: %v", err)
	}
	// index equal to length invalid for access, valid for add
	if _, err := s.Apply(1, []Operation{op("replace", "/arr/2", "30")}); !errors.Is(err, ErrInvalidIndex) {
		t.Fatalf("replace len: %v", err)
	}
	r, err = s.Apply(1, []Operation{op("add", "/arr/2", "30")})
	if err != nil || string(r.Document) != `{"":{"":1},"arr":[10,20,30]}` {
		t.Fatalf("add len: %+v %v", r, err)
	}
	// overflow index
	if _, err := s.Apply(2, []Operation{op("test", "/arr/99999999999999999999999999", "1")}); !errors.Is(err, ErrInvalidIndex) {
		t.Fatalf("overflow: %v", err)
	}
	// traversal through scalar
	if _, err := s.Apply(2, []Operation{op("test", "/arr/0/x", "1")}); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("scalar traversal: %v", err)
	}
}

func TestMoveRootAndDeepMoves(t *testing.T) {
	s := mustStore(t, `{"a":{"b":{"c":1}},"z":2}`, 30)
	// moving root anywhere non-root is a descendant move
	if _, err := s.Apply(1, []Operation{{Op: "move", From: "", Path: "/z/x"}}); !errors.Is(err, ErrMoveIntoChild) {
		t.Fatalf("root move: %v", err)
	}
	// deep move to root
	r, err := s.Apply(1, []Operation{{Op: "move", From: "/a/b/c", Path: ""}})
	if err != nil || string(r.Document) != "1" {
		t.Fatalf("deep to root: %+v %v", r, err)
	}
}

func TestMoveArrayShiftSemantics(t *testing.T) {
	s := mustStore(t, `["a","b","c","d"]`, 20)
	// removal happens before add: moving /0 to /3 appends at end
	r, err := s.Apply(1, []Operation{{Op: "move", From: "/0", Path: "/3"}})
	if err != nil || string(r.Document) != `["b","c","d","a"]` {
		t.Fatalf("move: %+v %v", r, err)
	}
}

func TestCopyIsDeepAndIndependent(t *testing.T) {
	s := mustStore(t, `{"a":{"x":[1]}}`, 30)
	if _, err := s.Apply(1, []Operation{{Op: "copy", From: "/a", Path: "/b"}}); err != nil {
		t.Fatal(err)
	}
	r, err := s.Apply(2, []Operation{op("add", "/b/x/-", "2")})
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Document) != `{"a":{"x":[1]},"b":{"x":[1,2]}}` {
		t.Fatalf("doc=%s", r.Document)
	}
}

func TestNumberLexemesPreserved(t *testing.T) {
	s := mustStore(t, `{"n":1.50,"e":1e2}`, 10)
	if string(s.Snapshot().Document) != `{"e":1e2,"n":1.50}` {
		t.Fatalf("doc=%s", s.Snapshot().Document)
	}
}

func TestTestOnlyBatchKeepsRevisionAndResult(t *testing.T) {
	s := mustStore(t, `{"a":1}`, 10)
	r, err := s.Apply(1, []Operation{op("test", "/a", "1.0")})
	if err != nil || r.Revision != 1 || r.Nodes != 2 || string(r.Document) != `{"a":1}` {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	// empty batch succeeds without revision change
	r, err = s.Apply(1, nil)
	if err != nil || r.Revision != 1 {
		t.Fatalf("empty=%+v err=%v", r, err)
	}
}

func TestCapacityExactlyAtLimit(t *testing.T) {
	s := mustStore(t, `{"a":1}`, 3) // currently 2 nodes
	r, err := s.Apply(1, []Operation{op("add", "/b", "2")})
	if err != nil || r.Nodes != 3 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if _, err := s.Apply(2, []Operation{op("add", "/c", "3")}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity=%v", err)
	}
}

func TestConcurrentMixedAccess(t *testing.T) {
	s := mustStore(t, `{"n":0}`, 10000)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				snap := s.Snapshot()
				_, _ = s.Apply(snap.Revision, []Operation{
					op("add", fmt.Sprintf("/k%d-%d", i, j), "1"),
					op("test", "/n", "0"),
				})
			}
		}()
	}
	wg.Wait()
	snap := s.Snapshot()
	// each successful batch adds exactly one node and bumps revision once
	if want := 2 + int(snap.Revision-1); snap.Nodes != want {
		t.Fatalf("nodes=%d want=%d rev=%d", snap.Nodes, want, snap.Revision)
	}
}

func TestResultIsolationAcrossCalls(t *testing.T) {
	s := mustStore(t, `{"a":1}`, 10)
	r1, err := s.Apply(1, []Operation{op("add", "/b", "2")})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.Apply(2, []Operation{op("add", "/c", "3")})
	if err != nil {
		t.Fatal(err)
	}
	r2.Document[0] = 'X'
	if string(r1.Document) != `{"a":1,"b":2}` {
		t.Fatalf("r1 mutated: %s", r1.Document)
	}
}
