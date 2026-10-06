package topologygraph288

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

// state is the mutable graph content guarded by Graph.mu.
type state struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
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
	for from, set := range s.out {
		ns := make(map[string]struct{}, len(set))
		for to := range set {
			ns[to] = struct{}{}
		}
		c.out[from] = ns
	}
	for to, set := range s.in {
		ns := make(map[string]struct{}, len(set))
		for from := range set {
			ns[from] = struct{}{}
		}
		c.in[to] = ns
	}
	return c
}

func (s *state) addNode(n string) {
	s.nodes[n] = struct{}{}
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

func (s *state) removeEdge(e Edge) {
	delete(s.edges, e)
	if set := s.out[e.From]; set != nil {
		delete(set, e.To)
		if len(set) == 0 {
			delete(s.out, e.From)
		}
	}
	if set := s.in[e.To]; set != nil {
		delete(set, e.From)
		if len(set) == 0 {
			delete(s.in, e.To)
		}
	}
}

func (s *state) removeNode(n string) {
	delete(s.nodes, n)
	for to := range s.out[n] {
		s.removeEdge(Edge{n, to})
	}
	for from := range s.in[n] {
		s.removeEdge(Edge{from, n})
	}
}

// reachable reports whether to is reachable from from via out-edges.
// Caller must ensure both nodes exist.
func (s *state) reachable(from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for next := range s.out[cur] {
			if next == to {
				return true
			}
			if _, ok := seen[next]; !ok {
				seen[next] = struct{}{}
				queue = append(queue, next)
			}
		}
	}
	return false
}

type Graph struct {
	mu         sync.RWMutex
	opts       Options
	st         *state
	generation uint64
}

func New(opts Options) (*Graph, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{opts: opts, st: newState()}, nil
}

// applyOp mutates st according to a single op, returning the first error.
func (s *state) applyOp(op Op) error {
	switch op.Kind {
	case AddNode:
		if _, ok := s.nodes[op.From]; ok {
			return ErrExists
		}
		s.addNode(op.From)
	case DeleteNode:
		if _, ok := s.nodes[op.From]; !ok {
			return ErrNotFound
		}
		s.removeNode(op.From)
	case AddEdge:
		e := Edge{op.From, op.To}
		if _, ok := s.nodes[op.From]; !ok {
			return ErrNotFound
		}
		if _, ok := s.nodes[op.To]; !ok {
			return ErrNotFound
		}
		if _, ok := s.edges[e]; ok {
			return ErrExists
		}
		if s.reachable(op.To, op.From) {
			return ErrCycle
		}
		s.addEdge(e)
	case DeleteEdge:
		e := Edge{op.From, op.To}
		if _, ok := s.edges[e]; !ok {
			return ErrNotFound
		}
		s.removeEdge(e)
	default:
		return ErrInvalidInput
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	cand := g.st.clone()
	for _, op := range b.Ops {
		if err := cand.applyOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(cand.nodes) > g.opts.MaxNodes || len(cand.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.st = cand
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
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
	nodes := make([]string, 0, len(g.st.nodes))
	for n := range g.st.nodes {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	edges := make([]Edge, 0, len(g.st.edges))
	for e := range g.st.edges {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return Snapshot{Generation: g.generation, Nodes: nodes, Edges: edges}
}
