package router

import (
	"errors"
	"net/url"
	"sort"
	"strings"
	"sync"
)

var (
	ErrNotImplemented = errors.New("router: not implemented")
	ErrInvalidOptions = errors.New("router: invalid options")
	ErrInvalidMethod  = errors.New("router: invalid method")
	ErrInvalidPattern = errors.New("router: invalid pattern")
	ErrInvalidPath    = errors.New("router: invalid path")
	ErrInvalidChange  = errors.New("router: invalid change")
	ErrInvalidParams  = errors.New("router: invalid params")
	ErrConflict       = errors.New("router: conflict")
	ErrNotFound       = errors.New("router: not found")
	ErrCapacity       = errors.New("router: capacity exceeded")
)

type Options struct{ MaxRoutes int }
type Route struct {
	Method, Pattern, Name string
	Value                 []byte
}
type ChangeType uint8

const (
	ChangeAdd ChangeType = iota + 1
	ChangeRemove
)

type Change struct {
	Type  ChangeType
	Route Route
	Name  string
}
type UpdateResult struct {
	Generation uint64
	Routes     int
}
type Param struct{ Name, Value string }
type MatchResult struct {
	Found            bool
	MethodNotAllowed bool
	HeadFallback     bool
	RouteName        string
	Value            []byte
	Params           []Param
	Allowed          []string
	Generation       uint64
}
type Snapshot struct {
	Generation uint64
	Routes     []Route
}

type segKind uint8

const (
	segStatic segKind = iota
	segParam
	segCatchAll
)

type segment struct {
	kind segKind
	lit  string // decoded static text
	name string // parameter name
}

type routeEntry struct {
	method  string
	pattern string
	name    string
	value   []byte
	segs    []segment
	shape   string
	params  []string // parameter names in pattern order
}

// nonCatchAll is the number of static+param segments.
func (e *routeEntry) nonCatchAll() int {
	n := 0
	for _, s := range e.segs {
		if s.kind != segCatchAll {
			n++
		}
	}
	return n
}

type Router struct {
	mu      sync.RWMutex
	max     int
	gen     uint64
	byName  map[string]*routeEntry
	byShape map[string]map[string]*routeEntry // method -> shape -> entry
}

func New(opts Options) (*Router, error) {
	if opts.MaxRoutes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Router{
		max:     opts.MaxRoutes,
		byName:  make(map[string]*routeEntry),
		byShape: make(map[string]map[string]*routeEntry),
	}, nil
}

func isTokenChar(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

// validMethod reports whether m is a non-empty uppercase HTTP token.
func validMethod(m string) bool {
	if m == "" {
		return false
	}
	for i := 0; i < len(m); i++ {
		c := m[i]
		if c >= 'a' && c <= 'z' || !isTokenChar(c) {
			return false
		}
	}
	return true
}

func validParamName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// parsePattern validates a pattern and returns its segments and shape key.
func parsePattern(pattern string) ([]segment, string, []string, error) {
	if !strings.HasPrefix(pattern, "/") {
		return nil, "", nil, ErrInvalidPattern
	}
	if pattern == "/" {
		return nil, "", nil, nil
	}
	raw := strings.Split(pattern[1:], "/")
	segs := make([]segment, 0, len(raw))
	var shape strings.Builder
	var params []string
	seen := make(map[string]bool)
	for i, rs := range raw {
		if rs == "" {
			return nil, "", nil, ErrInvalidPattern
		}
		var s segment
		switch rs[0] {
		case ':':
			name := rs[1:]
			if !validParamName(name) || seen[name] {
				return nil, "", nil, ErrInvalidPattern
			}
			seen[name] = true
			s = segment{kind: segParam, name: name}
			params = append(params, name)
			shape.WriteString("\x00P")
		case '*':
			name := rs[1:]
			if !validParamName(name) || seen[name] {
				return nil, "", nil, ErrInvalidPattern
			}
			if i != len(raw)-1 {
				return nil, "", nil, ErrInvalidPattern
			}
			seen[name] = true
			s = segment{kind: segCatchAll, name: name}
			params = append(params, name)
			shape.WriteString("\x00C")
		default:
			lit, err := url.PathUnescape(rs)
			if err != nil || lit == "" || lit == "." || lit == ".." || strings.Contains(lit, "/") {
				return nil, "", nil, ErrInvalidPattern
			}
			s = segment{kind: segStatic, lit: lit}
			shape.WriteString("\x00S\x00")
			shape.WriteString(lit)
		}
		segs = append(segs, s)
	}
	return segs, shape.String(), params, nil
}

func (r *Router) Apply(change Change) (UpdateResult, error) {
	return r.ApplyBatch([]Change{change})
}

func (r *Router) ApplyBatch(changes []Change) (UpdateResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(changes) == 0 {
		return UpdateResult{Generation: r.gen, Routes: len(r.byName)}, nil
	}
	byName := make(map[string]*routeEntry, len(r.byName)+len(changes))
	for k, v := range r.byName {
		byName[k] = v
	}
	byShape := make(map[string]map[string]*routeEntry, len(r.byShape))
	for m, shapes := range r.byShape {
		cp := make(map[string]*routeEntry, len(shapes))
		for k, v := range shapes {
			cp[k] = v
		}
		byShape[m] = cp
	}
	for _, ch := range changes {
		switch ch.Type {
		case ChangeAdd:
			rt := ch.Route
			if !validMethod(rt.Method) || rt.Method == "HEAD" {
				return UpdateResult{}, ErrInvalidMethod
			}
			segs, shape, params, err := parsePattern(rt.Pattern)
			if err != nil {
				return UpdateResult{}, err
			}
			if rt.Name == "" {
				return UpdateResult{}, ErrInvalidPattern
			}
			if _, ok := byName[rt.Name]; ok {
				return UpdateResult{}, ErrConflict
			}
			shapes := byShape[rt.Method]
			if shapes == nil {
				shapes = make(map[string]*routeEntry)
				byShape[rt.Method] = shapes
			}
			if _, ok := shapes[shape]; ok {
				return UpdateResult{}, ErrConflict
			}
			e := &routeEntry{
				method:  rt.Method,
				pattern: rt.Pattern,
				name:    rt.Name,
				value:   append([]byte(nil), rt.Value...),
				segs:    segs,
				shape:   shape,
				params:  params,
			}
			byName[e.name] = e
			shapes[shape] = e
		case ChangeRemove:
			e, ok := byName[ch.Name]
			if !ok {
				return UpdateResult{}, ErrNotFound
			}
			delete(byName, ch.Name)
			shapes := byShape[e.method]
			delete(shapes, e.shape)
			if len(shapes) == 0 {
				delete(byShape, e.method)
			}
		default:
			return UpdateResult{}, ErrInvalidChange
		}
	}
	if len(byName) > r.max {
		return UpdateResult{}, ErrCapacity
	}
	r.byName = byName
	r.byShape = byShape
	r.gen++
	return UpdateResult{Generation: r.gen, Routes: len(byName)}, nil
}

// decodePath validates an escaped request path and returns decoded segments.
func decodePath(escapedPath string) ([]string, error) {
	if !strings.HasPrefix(escapedPath, "/") {
		return nil, ErrInvalidPath
	}
	if escapedPath == "/" {
		return nil, nil
	}
	raw := strings.Split(escapedPath[1:], "/")
	segs := make([]string, 0, len(raw))
	for _, rs := range raw {
		if rs == "" {
			return nil, ErrInvalidPath
		}
		d, err := url.PathUnescape(rs)
		if err != nil || d == "" || d == "." || d == ".." || strings.Contains(d, "/") {
			return nil, ErrInvalidPath
		}
		segs = append(segs, d)
	}
	return segs, nil
}

// matchSegments returns params if the route matches the decoded segments.
func matchSegments(e *routeEntry, segs []string) ([]Param, bool) {
	var params []Param
	i := 0
	for _, s := range e.segs {
		switch s.kind {
		case segStatic:
			if i >= len(segs) || segs[i] != s.lit {
				return nil, false
			}
			i++
		case segParam:
			if i >= len(segs) {
				return nil, false
			}
			params = append(params, Param{Name: s.name, Value: segs[i]})
			i++
		case segCatchAll:
			if i >= len(segs) {
				return nil, false
			}
			params = append(params, Param{Name: s.name, Value: strings.Join(segs[i:], "/")})
			i = len(segs)
		}
	}
	if i != len(segs) {
		return nil, false
	}
	return params, true
}

// better reports whether candidate a outranks candidate b.
func better(a, b *routeEntry) bool {
	for i := 0; i < len(a.segs) && i < len(b.segs); i++ {
		if a.segs[i].kind != b.segs[i].kind {
			return a.segs[i].kind < b.segs[i].kind
		}
	}
	return a.nonCatchAll() > b.nonCatchAll()
}

func bestMatch(shapes map[string]*routeEntry, segs []string) (*routeEntry, []Param) {
	var best *routeEntry
	var bestParams []Param
	for _, e := range shapes {
		if params, ok := matchSegments(e, segs); ok {
			if best == nil || better(e, best) {
				best = e
				bestParams = params
			}
		}
	}
	return best, bestParams
}

func (r *Router) Match(method, escapedPath string) (MatchResult, error) {
	if !validMethod(method) {
		return MatchResult{}, ErrInvalidMethod
	}
	segs, err := decodePath(escapedPath)
	if err != nil {
		return MatchResult{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := MatchResult{Generation: r.gen}
	sel := method
	fallback := false
	if method == "HEAD" {
		sel = "GET"
		fallback = true
	}
	if shapes := r.byShape[sel]; shapes != nil {
		if e, params := bestMatch(shapes, segs); e != nil {
			res.Found = true
			res.HeadFallback = fallback
			res.RouteName = e.name
			res.Value = append([]byte(nil), e.value...)
			res.Params = params
			return res, nil
		}
	}
	allowed := make(map[string]bool)
	for m, shapes := range r.byShape {
		if m == sel {
			continue
		}
		if e, _ := bestMatch(shapes, segs); e != nil {
			allowed[m] = true
		}
	}
	if len(allowed) == 0 {
		return res, nil
	}
	if allowed["GET"] {
		allowed["HEAD"] = true
	}
	list := make([]string, 0, len(allowed))
	for m := range allowed {
		list = append(list, m)
	}
	sort.Strings(list)
	res.MethodNotAllowed = true
	res.Allowed = list
	return res, nil
}

func (r *Router) Build(name string, params map[string]string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byName[name]
	if !ok {
		return "", ErrNotFound
	}
	if len(params) != len(e.params) {
		return "", ErrInvalidParams
	}
	for _, p := range e.params {
		if _, ok := params[p]; !ok {
			return "", ErrInvalidParams
		}
	}
	if len(e.segs) == 0 {
		return "/", nil
	}
	parts := make([]string, 0, len(e.segs))
	for _, s := range e.segs {
		switch s.kind {
		case segStatic:
			parts = append(parts, url.PathEscape(s.lit))
		case segParam:
			v := params[s.name]
			if v == "" || v == "." || v == ".." || strings.Contains(v, "/") {
				return "", ErrInvalidParams
			}
			parts = append(parts, url.PathEscape(v))
		case segCatchAll:
			v := params[s.name]
			raw := strings.Split(v, "/")
			esc := make([]string, 0, len(raw))
			for _, rs := range raw {
				if rs == "" || rs == "." || rs == ".." {
					return "", ErrInvalidParams
				}
				esc = append(esc, url.PathEscape(rs))
			}
			parts = append(parts, strings.Join(esc, "/"))
		}
	}
	return "/" + strings.Join(parts, "/"), nil
}

func (r *Router) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	routes := make([]Route, 0, len(r.byName))
	for _, e := range r.byName {
		routes = append(routes, Route{
			Method:  e.method,
			Pattern: e.pattern,
			Name:    e.name,
			Value:   append([]byte(nil), e.value...),
		})
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Name < routes[j].Name })
	return Snapshot{Generation: r.gen, Routes: routes}
}
