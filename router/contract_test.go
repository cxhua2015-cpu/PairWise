package router

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func mustRouter(t *testing.T, n int) *Router {
	t.Helper()
	r, err := New(Options{MaxRoutes: n})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func add(t *testing.T, r *Router, route Route) {
	t.Helper()
	if _, err := r.Apply(Change{Type: ChangeAdd, Route: route}); err != nil {
		t.Fatal(err)
	}
}

func TestConstructionAndPatternValidation(t *testing.T) {
	if _, err := New(Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("New: %v", err)
	}
	r := mustRouter(t, 4)
	bad := []Route{{Method: "get", Pattern: "/a", Name: "a"}, {Method: "HEAD", Pattern: "/a", Name: "a"}, {Method: "GET", Pattern: "a", Name: "a"}, {Method: "GET", Pattern: "/a/", Name: "a"}, {Method: "GET", Pattern: "/:x/:x", Name: "a"}, {Method: "GET", Pattern: "/*x/a", Name: "a"}, {Method: "GET", Pattern: "/%2F", Name: "a"}}
	for i, route := range bad {
		if _, err := r.Apply(Change{Type: ChangeAdd, Route: route}); err == nil {
			t.Fatalf("bad[%d] accepted", i)
		}
	}
}

func TestPrecedenceIndependentOfRegistration(t *testing.T) {
	r := mustRouter(t, 8)
	add(t, r, Route{Method: "GET", Pattern: "/*rest", Name: "catch", Value: []byte("c")})
	add(t, r, Route{Method: "GET", Pattern: "/:x/info", Name: "param", Value: []byte("p")})
	add(t, r, Route{Method: "GET", Pattern: "/users/:id", Name: "user", Value: []byte("u")})
	add(t, r, Route{Method: "GET", Pattern: "/users/me", Name: "me", Value: []byte("m")})
	cases := map[string]string{"/users/me": "me", "/users/42": "user", "/team/info": "param", "/a/b/c": "catch"}
	for path, want := range cases {
		got, err := r.Match("GET", path)
		if err != nil || !got.Found || got.RouteName != want {
			t.Fatalf("%s: %+v %v", path, got, err)
		}
	}
}

func TestStructuralAndNameConflicts(t *testing.T) {
	r := mustRouter(t, 8)
	add(t, r, Route{Method: "GET", Pattern: "/u/:id", Name: "one"})
	if _, err := r.Apply(Change{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/u/:name", Name: "two"}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("shape: %v", err)
	}
	if _, err := r.Apply(Change{Type: ChangeAdd, Route: Route{Method: "POST", Pattern: "/other", Name: "one"}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("name: %v", err)
	}
	if _, err := r.Apply(Change{Type: ChangeAdd, Route: Route{Method: "POST", Pattern: "/u/:name", Name: "post"}}); err != nil {
		t.Fatalf("method-scoped shape: %v", err)
	}
}

func TestHeadFallbackAndAllowed(t *testing.T) {
	r := mustRouter(t, 8)
	add(t, r, Route{Method: "GET", Pattern: "/x/:id", Name: "get"})
	add(t, r, Route{Method: "POST", Pattern: "/x/:id", Name: "post"})
	m, err := r.Match("HEAD", "/x/a")
	if err != nil || !m.Found || !m.HeadFallback || m.RouteName != "get" {
		t.Fatalf("head=%+v err=%v", m, err)
	}
	m, err = r.Match("PUT", "/x/a")
	if err != nil || m.Found || !m.MethodNotAllowed || !reflect.DeepEqual(m.Allowed, []string{"GET", "HEAD", "POST"}) {
		t.Fatalf("allow=%+v err=%v", m, err)
	}
	m, _ = r.Match("PUT", "/none")
	if m.Found || m.MethodNotAllowed || len(m.Allowed) != 0 {
		t.Fatalf("none=%+v", m)
	}
}

func TestEscapingParamsAndTrailingSlash(t *testing.T) {
	r := mustRouter(t, 8)
	add(t, r, Route{Method: "GET", Pattern: "/doc/:name", Name: "doc"})
	add(t, r, Route{Method: "GET", Pattern: "/files/*path", Name: "files"})
	m, err := r.Match("GET", "/doc/a%20b")
	if err != nil || m.Params[0].Value != "a b" {
		t.Fatalf("m=%+v err=%v", m, err)
	}
	m, err = r.Match("GET", "/files/a/b%20c")
	if err != nil || m.Params[0].Value != "a/b c" {
		t.Fatalf("catch=%+v err=%v", m, err)
	}
	for _, path := range []string{"/doc/", "/doc/%2F", "/a//b", "relative", "/doc/%zz"} {
		if _, err := r.Match("GET", path); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("%q: %v", path, err)
		}
	}
}

func TestBuildRoundTripAndExactParams(t *testing.T) {
	r := mustRouter(t, 4)
	add(t, r, Route{Method: "GET", Pattern: "/repos/:owner/*path", Name: "repo"})
	p, err := r.Build("repo", map[string]string{"owner": "a b", "path": "x/y z"})
	if err != nil {
		t.Fatal(err)
	}
	if p != "/repos/a%20b/x/y%20z" {
		t.Fatalf("path=%q", p)
	}
	m, err := r.Match("GET", p)
	if err != nil || m.Params[0].Value != "a b" || m.Params[1].Value != "x/y z" {
		t.Fatalf("m=%+v err=%v", m, err)
	}
	for _, params := range []map[string]string{{"owner": "a"}, {"owner": "a", "path": "x", "extra": "y"}, {"owner": "a/b", "path": "x"}, {"owner": "a", "path": ""}} {
		if _, err := r.Build("repo", params); !errors.Is(err, ErrInvalidParams) {
			t.Fatalf("params=%v err=%v", params, err)
		}
	}
}

func TestBatchOrderTemporaryCapacityAndRollback(t *testing.T) {
	r := mustRouter(t, 1)
	add(t, r, Route{Method: "GET", Pattern: "/a", Name: "a", Value: []byte("a")})
	before := r.Snapshot()
	res, err := r.ApplyBatch([]Change{{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/b", Name: "b"}}, {Type: ChangeRemove, Name: "a"}})
	if err != nil || res.Routes != 1 || res.Generation != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err = r.ApplyBatch([]Change{{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/c", Name: "c"}}, {Type: ChangeRemove, Name: "missing"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rollback err=%v", err)
	}
	after := r.Snapshot()
	if after.Generation != 2 || len(after.Routes) != 1 || after.Routes[0].Name != "b" {
		t.Fatalf("after=%+v before=%+v", after, before)
	}
}

func TestOwnershipAndSnapshotOrder(t *testing.T) {
	r := mustRouter(t, 4)
	v := []byte("value")
	add(t, r, Route{Method: "GET", Pattern: "/z", Name: "z", Value: v})
	add(t, r, Route{Method: "GET", Pattern: "/a", Name: "a", Value: []byte("a")})
	v[0] = 'X'
	s := r.Snapshot()
	if s.Routes[0].Name != "a" || s.Routes[1].Name != "z" || string(s.Routes[1].Value) != "value" {
		t.Fatalf("s=%+v", s)
	}
	s.Routes[1].Value[0] = 'Y'
	m, _ := r.Match("GET", "/z")
	if !bytes.Equal(m.Value, []byte("value")) {
		t.Fatal("alias")
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	r := mustRouter(t, 2)
	res, err := r.ApplyBatch(nil)
	if err != nil || res.Generation != 0 || res.Routes != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err = r.Apply(Change{Type: 99}); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("err=%v", err)
	}
}

func TestConcurrentMatchBuildAndUpdates(t *testing.T) {
	r := mustRouter(t, 2000)
	add(t, r, Route{Method: "GET", Pattern: "/base/:id", Name: "base"})
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if g < 2 {
					name := fmt.Sprintf("n-%d-%d", g, i)
					_, err := r.Apply(Change{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: fmt.Sprintf("/s%d/x%d", g, i), Name: name}})
					if err != nil {
						t.Errorf("add: %v", err)
					}
				} else {
					m, err := r.Match("GET", "/base/x")
					if err != nil || !m.Found {
						t.Errorf("match: %+v %v", m, err)
					}
					if _, err := r.Build("base", map[string]string{"id": "x"}); err != nil {
						t.Errorf("build: %v", err)
					}
				}
			}
		}()
	}
	wg.Wait()
}
