package router

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestRootPatternAndPath(t *testing.T) {
	r := mustRouter(t, 4)
	add(t, r, Route{Method: "GET", Pattern: "/", Name: "root", Value: []byte("root")})
	m, err := r.Match("GET", "/")
	if err != nil || !m.Found || m.RouteName != "root" || len(m.Params) != 0 {
		t.Fatalf("m=%+v err=%v", m, err)
	}
	p, err := r.Build("root", map[string]string{})
	if err != nil || p != "/" {
		t.Fatalf("p=%q err=%v", p, err)
	}
	if _, err := r.Match("GET", "//"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err=%v", err)
	}
}

func TestPatternValidationEdges(t *testing.T) {
	r := mustRouter(t, 16)
	bad := []string{
		"", "a", "/a//b", "/a/", "/:", "/*", "/:1bad", "/:a-b", "/:x/:x",
		"/*x/a", "/%zz", "/%2F", "/.", "/..", "/a%2Fb",
	}
	for i, p := range bad {
		if _, err := r.Apply(Change{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: p, Name: fmt.Sprintf("n%d", i)}}); !errors.Is(err, ErrInvalidPattern) {
			t.Fatalf("pattern %q: %v", p, err)
		}
	}
	if _, err := r.Apply(Change{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/a", Name: ""}}); err == nil {
		t.Fatal("empty name accepted")
	}
	// Static segments are decoded at registration and matched decoded.
	add(t, r, Route{Method: "GET", Pattern: "/sp/a%20b", Name: "sp"})
	m, err := r.Match("GET", "/sp/a%20b")
	if err != nil || !m.Found || m.RouteName != "sp" {
		t.Fatalf("m=%+v err=%v", m, err)
	}
}

func TestMethodValidation(t *testing.T) {
	r := mustRouter(t, 4)
	for _, m := range []string{"", "get", "GE T", "G/ET", "HEAD", "G\x80T"} {
		if _, err := r.Apply(Change{Type: ChangeAdd, Route: Route{Method: m, Pattern: "/a", Name: "a"}}); !errors.Is(err, ErrInvalidMethod) {
			t.Fatalf("method %q: %v", m, err)
		}
	}
	for _, m := range []string{"", "get", "G ET"} {
		if _, err := r.Match(m, "/a"); !errors.Is(err, ErrInvalidMethod) {
			t.Fatalf("match method %q: %v", m, err)
		}
	}
	add(t, r, Route{Method: "GET", Pattern: "/a", Name: "a"})
	if _, err := r.Match("HEAD", "/a"); err != nil {
		t.Fatalf("HEAD match: %v", err)
	}
}

func TestMatchPathValidationEdges(t *testing.T) {
	r := mustRouter(t, 4)
	add(t, r, Route{Method: "GET", Pattern: "/a/:x", Name: "a"})
	for _, p := range []string{"", "rel", "/a/", "/a//b", "/a/%", "/a/%2f", "/a/.", "/a/..", "/a/%2E%2E"} {
		if _, err := r.Match("GET", p); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("path %q: %v", p, err)
		}
	}
}

func TestPrecedenceDeeperShapes(t *testing.T) {
	r := mustRouter(t, 8)
	add(t, r, Route{Method: "GET", Pattern: "/*all", Name: "all", Value: []byte("all")})
	add(t, r, Route{Method: "GET", Pattern: "/a/*rest", Name: "rest", Value: []byte("rest")})
	add(t, r, Route{Method: "GET", Pattern: "/a/:x", Name: "param", Value: []byte("param")})
	add(t, r, Route{Method: "GET", Pattern: "/a/b/c", Name: "static", Value: []byte("static")})
	cases := map[string]string{
		"/a/b/c":   "static",
		"/a/b":     "param",
		"/a/b/c/d": "rest",
		"/x/y":     "all",
	}
	for path, want := range cases {
		m, err := r.Match("GET", path)
		if err != nil || !m.Found || m.RouteName != want {
			t.Fatalf("%s: %+v %v", path, m, err)
		}
	}
}

func TestCatchAllRequiresOneSegment(t *testing.T) {
	r := mustRouter(t, 4)
	add(t, r, Route{Method: "GET", Pattern: "/f/*p", Name: "f"})
	m, err := r.Match("GET", "/f")
	if err != nil {
		t.Fatal(err)
	}
	if m.Found {
		t.Fatalf("catch-all matched zero segments: %+v", m)
	}
}

func TestAllowedHeadOnlyWithGet(t *testing.T) {
	r := mustRouter(t, 4)
	add(t, r, Route{Method: "POST", Pattern: "/x", Name: "p"})
	m, err := r.Match("PUT", "/x")
	if err != nil || !m.MethodNotAllowed || !reflect.DeepEqual(m.Allowed, []string{"POST"}) {
		t.Fatalf("m=%+v err=%v", m, err)
	}
	// HEAD request against POST-only route is also 405 without HEAD in Allow.
	m, err = r.Match("HEAD", "/x")
	if err != nil || !m.MethodNotAllowed || !reflect.DeepEqual(m.Allowed, []string{"POST"}) {
		t.Fatalf("m=%+v err=%v", m, err)
	}
}

func TestBatchNameAndShapeReuse(t *testing.T) {
	r := mustRouter(t, 4)
	add(t, r, Route{Method: "GET", Pattern: "/a/:id", Name: "a"})
	res, err := r.ApplyBatch([]Change{
		{Type: ChangeRemove, Name: "a"},
		{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/a/:other", Name: "a"}},
	})
	if err != nil || res.Routes != 1 || res.Generation != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	m, _ := r.Match("GET", "/a/1")
	if !m.Found || m.RouteName != "a" || m.Params[0].Name != "other" {
		t.Fatalf("m=%+v", m)
	}
}

func TestCapacityFinalOnly(t *testing.T) {
	r := mustRouter(t, 2)
	res, err := r.ApplyBatch([]Change{
		{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/a", Name: "a"}},
		{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/b", Name: "b"}},
		{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/c", Name: "c"}},
		{Type: ChangeRemove, Name: "a"},
		{Type: ChangeRemove, Name: "b"},
	})
	if err != nil || res.Routes != 1 || res.Generation != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err := r.Apply(Change{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/d", Name: "d"}}); err != nil {
		t.Fatalf("err=%v", err)
	}
	if _, err := r.Apply(Change{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/e", Name: "e"}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if s := r.Snapshot(); s.Generation != 2 || len(s.Routes) != 2 {
		t.Fatalf("s=%+v", s)
	}
}

func TestErrorLeavesStateUnchanged(t *testing.T) {
	r := mustRouter(t, 2)
	add(t, r, Route{Method: "GET", Pattern: "/a", Name: "a"})
	// Structural error precedes conflict: invalid pattern with colliding name.
	if _, err := r.Apply(Change{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "bad", Name: "a"}}); !errors.Is(err, ErrInvalidPattern) {
		t.Fatalf("err=%v", err)
	}
	// Batch with valid first change then error rolls back fully.
	if _, err := r.ApplyBatch([]Change{
		{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/b", Name: "b"}},
		{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/c", Name: "a"}},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
	s := r.Snapshot()
	if s.Generation != 1 || len(s.Routes) != 1 || s.Routes[0].Name != "a" {
		t.Fatalf("s=%+v", s)
	}
}

func TestBuildValidationEdges(t *testing.T) {
	r := mustRouter(t, 4)
	add(t, r, Route{Method: "GET", Pattern: "/f/*p", Name: "f"})
	if _, err := r.Build("missing", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	for _, v := range []string{"", "a//b", "a/./b", "..", "a/"} {
		if _, err := r.Build("f", map[string]string{"p": v}); !errors.Is(err, ErrInvalidParams) {
			t.Fatalf("p=%q err=%v", v, err)
		}
	}
	p, err := r.Build("f", map[string]string{"p": "a b/c?d"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := r.Match("GET", p)
	if err != nil || !m.Found || m.Params[0].Value != "a b/c?d" {
		t.Fatalf("p=%q m=%+v err=%v", p, m, err)
	}
}

func TestParamAndValueOwnership(t *testing.T) {
	r := mustRouter(t, 4)
	add(t, r, Route{Method: "GET", Pattern: "/a/:x", Name: "a", Value: []byte("v")})
	m1, _ := r.Match("GET", "/a/1")
	m1.Value[0] = 'X'
	m1.Params[0].Value = "mutated"
	m2, _ := r.Match("GET", "/a/1")
	if string(m2.Value) != "v" || m2.Params[0].Value != "1" {
		t.Fatalf("aliased: %+v", m2)
	}
	s1 := r.Snapshot()
	s1.Routes[0].Value[0] = 'Y'
	s2 := r.Snapshot()
	if string(s2.Routes[0].Value) != "v" {
		t.Fatalf("snapshot aliased: %+v", s2)
	}
}

func TestConcurrentBatchSnapshotAndRemove(t *testing.T) {
	r := mustRouter(t, 500)
	add(t, r, Route{Method: "GET", Pattern: "/stable/:id", Name: "stable"})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				switch g % 4 {
				case 0:
					name := fmt.Sprintf("tmp-%d-%d", g, i)
					_, err := r.ApplyBatch([]Change{
						{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: fmt.Sprintf("/tmp/%d/%d", g, i), Name: name}},
						{Type: ChangeRemove, Name: name},
					})
					if err != nil {
						t.Errorf("batch: %v", err)
					}
				case 1:
					m, err := r.Match("HEAD", "/stable/x")
					if err != nil || !m.Found || !m.HeadFallback {
						t.Errorf("match: %+v %v", m, err)
					}
				case 2:
					s := r.Snapshot()
					for i := 1; i < len(s.Routes); i++ {
						if s.Routes[i-1].Name >= s.Routes[i].Name {
							t.Errorf("snapshot not sorted")
							break
						}
					}
				case 3:
					if _, err := r.Build("stable", map[string]string{"id": "a b"}); err != nil {
						t.Errorf("build: %v", err)
					}
				}
			}
		}()
	}
	wg.Wait()
	s := r.Snapshot()
	if len(s.Routes) != 1 || s.Routes[0].Name != "stable" {
		t.Fatalf("s=%+v", s)
	}
}
