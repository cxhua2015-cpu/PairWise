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
	kind   segKind
	static string // decoded static text
	name   string // param / catch-all name
}

func (s segment) rank() int {
	switch s.kind {
	case segStatic:
		return 0
	case segParam:
		return 1
	default:
		return 2
	}
}

type routeEntry struct {
	method  string
	pattern string
	name    string
	value   []byte
	segs    []segment
	params  []string // parameter names in pattern order
	shape   string   // structural key used for conflict detection
}

func (e *routeEntry) nonCatchAll() int {
	n := len(e.segs)
	if n > 0 && e.segs[n-1].kind == segCatchAll {
		n--
	}
	return n
}

// compare orders routes by matching precedence: lower wins.
func compareEntries(a, b *routeEntry) int {
	n := len(a.segs)
	if len(b.segs) < n {
		n = len(b.segs)
	}
	for i := 0; i < n; i++ {
		ra, rb := a.segs[i].rank(), b.segs[i].rank()
		if ra != rb {
			return ra - rb
		}
	}
	if ca, cb := a.nonCatchAll(), b.nonCatchAll(); ca != cb {
		return cb - ca // more non-catch-all segments wins
	}
	return 0
}

// match reports whether the decoded segments match, and returns params in pattern order.
func (e *routeEntry) match(segs []string) ([]Param, bool) {
	var out []Param
	i := 0
	for _, s := range e.segs {
		switch s.kind {
		case segStatic:
			if i >= len(segs) || segs[i] != s.static {
				return nil, false
			}
			i++
		case segParam:
			if i >= len(segs) {
				return nil, false
			}
			out = append(out, Param{Name: s.name, Value: segs[i]})
			i++
		case segCatchAll:
			if len(segs)-i < 1 {
				return nil, false
			}
			out = append(out, Param{Name: s.name, Value: strings.Join(segs[i:], "/")})
			i = len(segs)
		}
	}
	if i != len(segs) {
		return nil, false
	}
	return out, true
}

type state struct {
	byName   map[string]*routeEntry
	byMethod map[string][]*routeEntry
	shapes   map[string]map[string]struct{} // method -> set of shapes
	count    int
}

func newState() *state {
	return &state{
		byName:   make(map[string]*routeEntry),
		byMethod: make(map[string][]*routeEntry),
		shapes:   make(map[string]map[string]struct{}),
	}
}

func (s *state) clone() *state {
	c := newState()
	c.count = s.count
	for k, v := range s.byName {
		c.byName[k] = v
	}
	for k, v := range s.byMethod {
		c.byMethod[k] = append([]*routeEntry(nil), v...)
	}
	for k, v := range s.shapes {
		m := make(map[string]struct{}, len(v))
		for sh := range v {
			m[sh] = struct{}{}
		}
		c.shapes[k] = m
	}
	return c
}

func (s *state) add(e *routeEntry) {
	s.byName[e.name] = e
	s.byMethod[e.method] = append(s.byMethod[e.method], e)
	if s.shapes[e.method] == nil {
		s.shapes[e.method] = make(map[string]struct{})
	}
	s.shapes[e.method][e.shape] = struct{}{}
	s.count++
}

func (s *state) remove(name string) {
	e := s.byName[name]
	delete(s.byName, name)
	list := s.byMethod[e.method]
	for i, r := range list {
		if r == e {
			s.byMethod[e.method] = append(list[:i], list[i+1:]...)
			break
		}
	}
	delete(s.shapes[e.method], e.shape)
	s.count--
}

type Router struct {
	mu         sync.RWMutex
	maxRoutes  int
	generation uint64
	st         *state
}

func New(opts Options) (*Router, error) {
	if opts.MaxRoutes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Router{maxRoutes: opts.MaxRoutes, st: newState()}, nil
}

func validMethod(m string) bool {
	if m == "" {
		return false
	}
	for i := 0; i < len(m); i++ {
		c := m[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
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
		ok := c == '_' ||
			(c >= 'A' && c <= 'Z') ||
			(c >= 'a' && c <= 'z') ||
			(i > 0 && c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

func validDecodedSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.Contains(s, "/")
}

// parsePattern validates a pattern and builds its segment list and shape key.
func parsePattern(pattern string) ([]segment, []string, string, error) {
	if !strings.HasPrefix(pattern, "/") {
		return nil, nil, "", ErrInvalidPattern
	}
	if pattern == "/" {
		return nil, nil, "/", nil
	}
	parts := strings.Split(pattern[1:], "/")
	segs := make([]segment, 0, len(parts))
	var params []string
	var shape strings.Builder
	seen := make(map[string]struct{})
	for i, p := range parts {
		if p == "" {
			return nil, nil, "", ErrInvalidPattern
		}
		var s segment
		switch p[0] {
		case ':':
			name := p[1:]
			if !validParamName(name) {
				return nil, nil, "", ErrInvalidPattern
			}
			if _, dup := seen[name]; dup {
				return nil, nil, "", ErrInvalidPattern
			}
			seen[name] = struct{}{}
			s = segment{kind: segParam, name: name}
			params = append(params, name)
			shape.WriteString("\x00:")
		case '*':
			name := p[1:]
			if !validParamName(name) || i != len(parts)-1 {
				return nil, nil, "", ErrInvalidPattern
			}
			if _, dup := seen[name]; dup {
				return nil, nil, "", ErrInvalidPattern
			}
			seen[name] = struct{}{}
			s = segment{kind: segCatchAll, name: name}
			params = append(params, name)
			shape.WriteString("\x00*")
		default:
			dec, err := url.PathUnescape(p)
			if err != nil || !validDecodedSegment(dec) {
				return nil, nil, "", ErrInvalidPattern
			}
			s = segment{kind: segStatic, static: dec}
			shape.WriteString("\x00s")
			shape.WriteString(dec)
		}
		segs = append(segs, s)
	}
	return segs, params, shape.String(), nil
}

// parseRequestPath validates and decodes an escaped request path.
func parseRequestPath(escapedPath string) ([]string, error) {
	if !strings.HasPrefix(escapedPath, "/") {
		return nil, ErrInvalidPath
	}
	if escapedPath == "/" {
		return nil, nil
	}
	parts := strings.Split(escapedPath[1:], "/")
	segs := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			return nil, ErrInvalidPath
		}
		dec, err := url.PathUnescape(p)
		if err != nil || !validDecodedSegment(dec) {
			return nil, ErrInvalidPath
		}
		segs = append(segs, dec)
	}
	return segs, nil
}

func (r *Router) Apply(change Change) (UpdateResult, error) {
	return r.ApplyBatch([]Change{change})
}

func (r *Router) ApplyBatch(changes []Change) (UpdateResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(changes) == 0 {
		return UpdateResult{Generation: r.generation, Routes: r.st.count}, nil
	}
	cand := r.st.clone()
	for _, c := range changes {
		switch c.Type {
		case ChangeAdd:
			rt := c.Route
			if !validMethod(rt.Method) || rt.Method == "HEAD" {
				return UpdateResult{}, ErrInvalidMethod
			}
			segs, params, shape, err := parsePattern(rt.Pattern)
			if err != nil {
				return UpdateResult{}, err
			}
			if rt.Name == "" {
				return UpdateResult{}, ErrInvalidPattern
			}
			if _, ok := cand.byName[rt.Name]; ok {
				return UpdateResult{}, ErrConflict
			}
			if _, ok := cand.shapes[rt.Method][shape]; ok {
				return UpdateResult{}, ErrConflict
			}
			e := &routeEntry{
				method:  rt.Method,
				pattern: rt.Pattern,
				name:    rt.Name,
				segs:    segs,
				params:  params,
				shape:   shape,
			}
			if rt.Value != nil {
				e.value = append([]byte(nil), rt.Value...)
			}
			cand.add(e)
		case ChangeRemove:
			if _, ok := cand.byName[c.Name]; !ok {
				return UpdateResult{}, ErrNotFound
			}
			cand.remove(c.Name)
		default:
			return UpdateResult{}, ErrInvalidChange
		}
	}
	if cand.count > r.maxRoutes {
		return UpdateResult{}, ErrCapacity
	}
	r.st = cand
	r.generation++
	return UpdateResult{Generation: r.generation, Routes: cand.count}, nil
}

// bestMatch returns the highest-precedence matching entry for a method list.
func bestMatch(list []*routeEntry, segs []string) (*routeEntry, []Param) {
	var best *routeEntry
	var bestParams []Param
	for _, e := range list {
		p, ok := e.match(segs)
		if !ok {
			continue
		}
		if best == nil || compareEntries(e, best) < 0 {
			best, bestParams = e, p
		}
	}
	return best, bestParams
}

func (r *Router) Match(method, escapedPath string) (MatchResult, error) {
	if !validMethod(method) {
		return MatchResult{}, ErrInvalidMethod
	}
	segs, err := parseRequestPath(escapedPath)
	if err != nil {
		return MatchResult{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := MatchResult{Generation: r.generation}

	tryMethods := []string{method}
	if method == "HEAD" {
		tryMethods = append(tryMethods, "GET")
	}
	for i, m := range tryMethods {
		if e, params := bestMatch(r.st.byMethod[m], segs); e != nil {
			res.Found = true
			res.RouteName = e.name
			res.Params = params
			if e.value != nil {
				res.Value = append([]byte(nil), e.value...)
			}
			if i > 0 {
				res.HeadFallback = true
			}
			return res, nil
		}
	}
	allowed := make(map[string]struct{})
	for m, list := range r.st.byMethod {
		if e, _ := bestMatch(list, segs); e != nil {
			allowed[m] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return res, nil
	}
	if _, ok := allowed["GET"]; ok {
		allowed["HEAD"] = struct{}{}
	}
	res.MethodNotAllowed = true
	res.Allowed = make([]string, 0, len(allowed))
	for m := range allowed {
		res.Allowed = append(res.Allowed, m)
	}
	sort.Strings(res.Allowed)
	return res, nil
}

func (r *Router) Build(name string, params map[string]string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.st.byName[name]
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
	var b strings.Builder
	for _, s := range e.segs {
		b.WriteByte('/')
		switch s.kind {
		case segStatic:
			b.WriteString(url.PathEscape(s.static))
		case segParam:
			v := params[s.name]
			if !validDecodedSegment(v) {
				return "", ErrInvalidParams
			}
			b.WriteString(url.PathEscape(v))
		case segCatchAll:
			v := params[s.name]
			parts := strings.Split(v, "/")
			for i, part := range parts {
				if !validDecodedSegment(part) {
					return "", ErrInvalidParams
				}
				if i > 0 {
					b.WriteByte('/')
				}
				b.WriteString(url.PathEscape(part))
			}
		}
	}
	out := b.String()
	if out == "" {
		out = "/"
	}
	return out, nil
}

func (r *Router) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := Snapshot{Generation: r.generation, Routes: make([]Route, 0, r.st.count)}
	for _, e := range r.st.byName {
		rt := Route{Method: e.method, Pattern: e.pattern, Name: e.name}
		if e.value != nil {
			rt.Value = append([]byte(nil), e.value...)
		}
		s.Routes = append(s.Routes, rt)
	}
	sort.Slice(s.Routes, func(i, j int) bool { return s.Routes[i].Name < s.Routes[j].Name })
	return s
}
