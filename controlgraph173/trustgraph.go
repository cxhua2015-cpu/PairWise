package controlgraph173

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrExists         = errors.New("already exists")
	ErrNotFound       = errors.New("not found")
	ErrCycle          = errors.New("cycle")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	AddNode Kind = iota + 1
	DeleteNode
	AddEdge
	DeleteEdge
)

type Options struct{ MaxNodes, MaxEdges, MaxNameBytes int }
type Op struct {
	Kind     Kind
	From, To string
}
type Batch struct{ Ops []Op }
type Edge struct{ From, To string }
type Result struct{ Generation uint64 }
type Snapshot struct {
	Generation uint64
	Nodes      []string
	Edges      []Edge
}

type Graph struct {
	mu    sync.RWMutex
	opts  Options
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
	gen   uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  o,
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		out:   make(map[string]map[string]struct{}),
		in:    make(map[string]map[string]struct{}),
	}, nil
}

func validName(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validate checks structural correctness of every op before any state is read.
func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, g.opts.MaxNameBytes) {
				return ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.opts.MaxNameBytes) || !validName(op.To, g.opts.MaxNameBytes) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// txn is a candidate transaction: mutations are staged here and only
// committed to the graph when the whole batch succeeds.
type txn struct {
	g     *Graph
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

func (g *Graph) begin() *txn {
	t := &txn{
		g:     g,
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
		in:    make(map[string]map[string]struct{}, len(g.in)),
	}
	for k := range g.nodes {
		t.nodes[k] = struct{}{}
	}
	for k := range g.edges {
		t.edges[k] = struct{}{}
	}
	for k, v := range g.out {
		s := make(map[string]struct{}, len(v))
		for x := range v {
			s[x] = struct{}{}
		}
		t.out[k] = s
	}
	for k, v := range g.in {
		s := make(map[string]struct{}, len(v))
		for x := range v {
			s[x] = struct{}{}
		}
		t.in[k] = s
	}
	return t
}

func (t *txn) addEdge(e Edge) {
	t.edges[e] = struct{}{}
	if t.out[e.From] == nil {
		t.out[e.From] = make(map[string]struct{})
	}
	t.out[e.From][e.To] = struct{}{}
	if t.in[e.To] == nil {
		t.in[e.To] = make(map[string]struct{})
	}
	t.in[e.To][e.From] = struct{}{}
}

func (t *txn) delEdge(e Edge) {
	delete(t.edges, e)
	if s := t.out[e.From]; s != nil {
		delete(s, e.To)
		if len(s) == 0 {
			delete(t.out, e.From)
		}
	}
	if s := t.in[e.To]; s != nil {
		delete(s, e.From)
		if len(s) == 0 {
			delete(t.in, e.To)
		}
	}
}

// reachable reports whether target is reachable from src in the staged state.
func (t *txn) reachable(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range t.out[n] {
			if m == dst {
				return true
			}
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				stack = append(stack, m)
			}
		}
	}
	return false
}

func (t *txn) apply(op Op) error {
	switch op.Kind {
	case AddNode:
		if _, ok := t.nodes[op.From]; ok {
			return ErrExists
		}
		t.nodes[op.From] = struct{}{}
	case DeleteNode:
		if _, ok := t.nodes[op.From]; !ok {
			return ErrNotFound
		}
		delete(t.nodes, op.From)
		for to := range t.out[op.From] {
			t.delEdge(Edge{op.From, to})
		}
		for from := range t.in[op.From] {
			t.delEdge(Edge{from, op.From})
		}
	case AddEdge:
		e := Edge{op.From, op.To}
		if _, ok := t.nodes[op.From]; !ok {
			return ErrNotFound
		}
		if _, ok := t.nodes[op.To]; !ok {
			return ErrNotFound
		}
		if _, ok := t.edges[e]; ok {
			return ErrExists
		}
		if t.reachable(op.To, op.From) {
			return ErrCycle
		}
		t.addEdge(e)
	case DeleteEdge:
		e := Edge{op.From, op.To}
		if _, ok := t.edges[e]; !ok {
			return ErrNotFound
		}
		t.delEdge(e)
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.validate(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}
	t := g.begin()
	for _, op := range b.Ops {
		if err := t.apply(op); err != nil {
			return Result{}, err
		}
	}
	if len(t.nodes) > g.opts.MaxNodes || len(t.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges, g.out, g.in = t.nodes, t.edges, t.out, t.in
	g.gen++
	return Result{Generation: g.gen}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.opts.MaxNameBytes) || !validName(to, g.opts.MaxNameBytes) {
		return false, ErrInvalidInput
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return false, ErrNotFound
	}
	t := &txn{out: g.out}
	return t.reachable(from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := Snapshot{
		Generation: g.gen,
		Nodes:      make([]string, 0, len(g.nodes)),
		Edges:      make([]Edge, 0, len(g.edges)),
	}
	for n := range g.nodes {
		s.Nodes = append(s.Nodes, n)
	}
	for e := range g.edges {
		s.Edges = append(s.Edges, e)
	}
	sort.Strings(s.Nodes)
	sort.Slice(s.Edges, func(i, j int) bool {
		if s.Edges[i].From != s.Edges[j].From {
			return s.Edges[i].From < s.Edges[j].From
		}
		return s.Edges[i].To < s.Edges[j].To
	})
	return s
}
