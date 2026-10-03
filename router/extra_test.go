package router

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestRootPatternAndCatchAllMinimum(t *testing.T) {
	r := mustRouter(t, 4)
	add(t, r, Route{Method: "GET", Pattern: "/", Name: "root", Value: []byte("root")})
	add(t, r, Route{Method: "GET", Pattern: "/files/*path", Name: "files"})
	m, err := r.Match("GET", "/")
	if err != nil || !m.Found || m.RouteName != "root" {
		t.Fatalf("root=%+v err=%v", m, err)
	}
	if p, err := r.Build("root", nil); err != nil || p != "/" {
		t.Fatalf("build root=%q err=%v", p, err)
	}
	// catch-all requires at least one segment
	if m, _ := r.Match("GET", "/files"); m.Found {
		t.Fatalf("catch-all matched zero segments: %+v", m)
	}
	if m, _ := r.Match("GET", "/files/x"); !m.Found || m.RouteName != "files" {
		t.Fatalf("catch-all one segment: %+v", m)
	}
}

func TestStaticDecodedMatchingAndEscapes(t *testing.T) {
	r := mustRouter(t, 4)
	add(t, r, Route{Method: "GET", Pattern: "/a%20b/c", Name: "sp"})
	for _, path := range []string{"/a%20b/c", "/a b/c"} {
		m, err := r.Match("GET", path)
		if err != nil || !m.Found || m.RouteName != "sp" {
			t.Fatalf("%q: %+v err=%v", path, m, err)
		}
	}
	// escaped static that decodes to dot segments is rejected
	for _, pat := range []string{"/%2E", "/%2E%2E", "/a//b", "/a/", "", "x", "/:", "/*", "/:1bad", "/:x/:x", "/a/*x/*y"} {
		if _, err := r.Apply(Change{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: pat, Name: "n" + pat}}); !errors.Is(err, ErrInvalidPattern) {
			t.Fatalf("pattern %q: %v", pat, err)
		}
	}
}

func TestMethodValidation(t *testing.T) {
	r := mustRouter(t, 4)
	for _, m := range []string{"", "get", "GeT", "G ET", "G/ET", "HEAD"} {
		if _, err := r.Apply(Change{Type: ChangeAdd, Route: Route{Method: m, Pattern: "/a", Name: "a"}}); !errors.Is(err, ErrInvalidMethod) {
			t.Fatalf("method %q: %v", m, err)
		}
	}
	if _, err := r.Match("get", "/a"); !errors.Is(err, ErrInvalidMethod) {
		t.Fatalf("match method: %v", err)
	}
	if _, err := r.Match("HEAD", "/a"); err != nil {
		t.Fatalf("HEAD request must be accepted: %v", err)
	}
}

func TestBatchNameReuseAndOrder(t *testing.T) {
	r := mustRouter(t, 2)
	add(t, r, Route{Method: "GET", Pattern: "/a/:id", Name: "a"})
	// remove frees both the name and the shape for reuse within one batch
	res, err := r.ApplyBatch([]Change{
		{Type: ChangeRemove, Name: "a"},
		{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/a/:id", Name: "a", Value: []byte("v2")}},
	})
	if err != nil || res.Generation != 2 || res.Routes != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	m, _ := r.Match("GET", "/a/1")
	if string(m.Value) != "v2" {
		t.Fatalf("m=%+v", m)
	}
	// error leaves generation and routes untouched
	if _, err := r.ApplyBatch([]Change{
		{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/b", Name: "b"}},
		{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/b", Name: "b2"}},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
	s := r.Snapshot()
	if s.Generation != 2 || len(s.Routes) != 1 {
		t.Fatalf("s=%+v", s)
	}
}

func TestCapacityCheckedOnlyAtEnd(t *testing.T) {
	r := mustRouter(t, 1)
	add(t, r, Route{Method: "GET", Pattern: "/a", Name: "a"})
	// temporarily 2 routes, final 1: allowed
	if _, err := r.ApplyBatch([]Change{
		{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/b", Name: "b"}},
		{Type: ChangeRemove, Name: "a"},
		{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/c", Name: "c"}},
		{Type: ChangeRemove, Name: "b"}},
	); err != nil {
		t.Fatalf("err=%v", err)
	}
	// final overflow rejected and rolled back
	if _, err := r.ApplyBatch([]Change{
		{Type: ChangeAdd, Route: Route{Method: "GET", Pattern: "/d", Name: "d"}},
	}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	s := r.Snapshot()
	if len(s.Routes) != 1 || s.Routes[0].Name != "c" {
		t.Fatalf("s=%+v", s)
	}
}

func TestBuildEscapesAndRoundTrip(t *testing.T) {
	r := mustRouter(t, 4)
	add(t, r, Route{Method: "GET", Pattern: "/x/:p/*rest", Name: "x"})
	p, err := r.Build("x", map[string]string{"p": "a?b&c", "rest": "d e/é"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := r.Match("GET", p)
	if err != nil || !m.Found {
		t.Fatalf("round trip p=%q err=%v", p, err)
	}
	want := []Param{{Name: "p", Value: "a?b&c"}, {Name: "rest", Value: "d e/é"}}
	if !reflect.DeepEqual(m.Params, want) {
		t.Fatalf("params=%v want=%v", m.Params, want)
	}
	for _, params := range []map[string]string{
		{"p": ".", "rest": "x"}, {"p": "..", "rest": "x"}, {"p": "", "rest": "x"},
		{"p": "a", "rest": "."}, {"p": "a", "rest": "x/"}, {"p": "a", "rest": "/x"},
	} {
		if _, err := r.Build("x", params); !errors.Is(err, ErrInvalidParams) {
			t.Fatalf("params=%v err=%v", params, err)
		}
	}
	if _, err := r.Build("missing", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestAllowedSortedAndHeadAdded(t *testing.T) {
	r := mustRouter(t, 8)
	add(t, r, Route{Method: "POST", Pattern: "/r", Name: "p"})
	add(t, r, Route{Method: "GET", Pattern: "/r", Name: "g"})
	add(t, r, Route{Method: "DELETE", Pattern: "/r", Name: "d"})
	m, err := r.Match("PUT", "/r")
	if err != nil || m.MethodNotAllowed == false {
		t.Fatalf("m=%+v err=%v", m, err)
	}
	if !reflect.DeepEqual(m.Allowed, []string{"DELETE", "GET", "HEAD", "POST"}) {
		t.Fatalf("allowed=%v", m.Allowed)
	}
	// without GET, HEAD is not advertised
	r2 := mustRouter(t, 4)
	add(t, r2, Route{Method: "POST", Pattern: "/r", Name: "p"})
	m, _ = r2.Match("PUT", "/r")
	if !reflect.DeepEqual(m.Allowed, []string{"POST"}) {
		t.Fatalf("allowed=%v", m.Allowed)
	}
}

func TestParamAndSnapshotIsolation(t *testing.T) {
	r := mustRouter(t, 4)
	add(t, r, Route{Method: "GET", Pattern: "/i/:id", Name: "i", Value: []byte("v")})
	m1, _ := r.Match("GET", "/i/1")
	m1.Value[0] = 'X'
	m1.Params[0].Value = "mutated"
	m2, _ := r.Match("GET", "/i/2")
	if string(m2.Value) != "v" || m2.Params[0].Value != "2" {
		t.Fatalf("isolation: %+v", m2)
	}
	s1 := r.Snapshot()
	s1.Routes[0].Value[0] = 'Y'
	s2 := r.Snapshot()
	if string(s2.Routes[0].Value) != "v" {
		t.Fatalf("snapshot alias: %+v", s2.Routes[0])
	}
}

func TestConcurrentBatchMatchSnapshot(t *testing.T) {
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
					m, err := r.Match("HEAD", "/stable/abc")
					if err != nil || !m.Found || !m.HeadFallback {
						t.Errorf("match: %+v %v", m, err)
					}
				case 2:
					s := r.Snapshot()
					for _, rt := range s.Routes {
						if _, err := r.Match(rt.Method, "/stable/abc"); err != nil {
							t.Errorf("match: %v", err)
						}
					}
				case 3:
					if _, err := r.Build("stable", map[string]string{"id": "x y"}); err != nil {
						t.Errorf("build: %v", err)
					}
				}
			}
		}()
	}
	wg.Wait()
	if _, err := r.Match("GET", "/stable/abc"); err != nil {
		t.Fatal(err)
	}
}
