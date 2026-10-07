package topologygraph328

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

type state struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

type Graph struct {
	mu   sync.RWMutex
	opts Options
	st   *state
	gen  uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{opts: o, st: newState()}, nil
}

func newState() *state {
	return &state{
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		out:   make(map[string]map[string]struct{}),
		in:    make(map[string]map[string]struct{}),
	}
}

func (s *state) clone() *state {
	c := newState()
	for n := range s.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range s.edges {
		c.edges[e] = struct{}{}
	}
	for k, m := range s.out {
		cm := make(map[string]struct{}, len(m))
		for v := range m {
			cm[v] = struct{}{}
		}
		c.out[k] = cm
	}
	for k, m := range s.in {
		cm := make(map[string]struct{}, len(m))
		for v := range m {
			cm[v] = struct{}{}
		}
		c.in[k] = cm
	}
	return c
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

func (s *state) addEdge(e Edge) {
	s.edges[e] = struct{}{}
	if s.out[e.From] == nil {
		s.out[e.From] = make(map[string]struct{})
	}
	s.out[e.From][e.To] = struct{}{}
	if s.in[e.To] == nil {
		s.in[e.To] = make(map[string]struct{})
	}
	s.in[e.To][e.From] = struct{}{}
}

func (s *state) delEdge(e Edge) {
	delete(s.edges, e)
	delete(s.out[e.From], e.To)
	delete(s.in[e.To], e.From)
}

func (s *state) delNode(n string) {
	delete(s.nodes, n)
	for to := range s.out[n] {
		s.delEdge(Edge{n, to})
	}
	for from := range s.in[n] {
		s.delEdge(Edge{from, n})
	}
	delete(s.out, n)
	delete(s.in, n)
}

// reachable reports whether to is reachable from from, assuming both exist.
func (s *state) reachable(from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range s.out[cur] {
			if next == to {
				return true
			}
			if _, ok := seen[next]; !ok {
				seen[next] = struct{}{}
				stack = append(stack, next)
			}
		}
	}
	return false
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
	cand := g.st.clone()
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := cand.nodes[op.From]; ok {
				return Result{}, ErrExists
			}
			cand.nodes[op.From] = struct{}{}
		case DeleteNode:
			if _, ok := cand.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			cand.delNode(op.From)
		case AddEdge:
			e := Edge{op.From, op.To}
			if _, ok := cand.nodes[e.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.nodes[e.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.edges[e]; ok {
				return Result{}, ErrExists
			}
			if cand.reachable(e.To, e.From) {
				return Result{}, ErrCycle
			}
			cand.addEdge(e)
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := cand.edges[e]; !ok {
				return Result{}, ErrNotFound
			}
			cand.delEdge(e)
		}
	}
	if len(cand.nodes) > g.opts.MaxNodes || len(cand.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.st = cand
	g.gen++
	return Result{Generation: g.gen}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.opts.MaxNameBytes) || !validName(to, g.opts.MaxNameBytes) {
		return false, ErrInvalidInput
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.st.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.st.nodes[to]; !ok {
		return false, ErrNotFound
	}
	return g.st.reachable(from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	snap := Snapshot{
		Generation: g.gen,
		Nodes:      make([]string, 0, len(g.st.nodes)),
		Edges:      make([]Edge, 0, len(g.st.edges)),
	}
	for n := range g.st.nodes {
		snap.Nodes = append(snap.Nodes, n)
	}
	sort.Strings(snap.Nodes)
	for e := range g.st.edges {
		snap.Edges = append(snap.Edges, e)
	}
	sort.Slice(snap.Edges, func(i, j int) bool {
		if snap.Edges[i].From != snap.Edges[j].From {
			return snap.Edges[i].From < snap.Edges[j].From
		}
		return snap.Edges[i].To < snap.Edges[j].To
	})
	return snap
}
