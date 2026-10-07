package topologygraph383

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
// state holds the graph indexes. nodes is the set of node names;
// edges is the set of directed edges; out and in are adjacency
// indexes mapping a node to its successors / predecessors.
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
	for n, succs := range s.out {
		m := make(map[string]struct{}, len(succs))
		for to := range succs {
			m[to] = struct{}{}
		}
		c.out[n] = m
	}
	for n, preds := range s.in {
		m := make(map[string]struct{}, len(preds))
		for from := range preds {
			m[from] = struct{}{}
		}
		c.in[n] = m
	}
	return c
}

func (s *state) addNode(name string) error {
	if _, ok := s.nodes[name]; ok {
		return ErrExists
	}
	s.nodes[name] = struct{}{}
	return nil
}

func (s *state) deleteNode(name string) error {
	if _, ok := s.nodes[name]; !ok {
		return ErrNotFound
	}
	for to := range s.out[name] {
		delete(s.edges, Edge{From: name, To: to})
		delete(s.in[to], name)
	}
	delete(s.out, name)
	for from := range s.in[name] {
		delete(s.edges, Edge{From: from, To: name})
		delete(s.out[from], name)
	}
	delete(s.in, name)
	delete(s.nodes, name)
	return nil
}

func (s *state) addEdge(from, to string) error {
	if _, ok := s.nodes[from]; !ok {
		return ErrNotFound
	}
	if _, ok := s.nodes[to]; !ok {
		return ErrNotFound
	}
	if _, ok := s.edges[Edge{From: from, To: to}]; ok {
		return ErrExists
	}
	if s.reachable(to, from) {
		return ErrCycle
	}
	s.edges[Edge{From: from, To: to}] = struct{}{}
	if s.out[from] == nil {
		s.out[from] = make(map[string]struct{})
	}
	s.out[from][to] = struct{}{}
	if s.in[to] == nil {
		s.in[to] = make(map[string]struct{})
	}
	s.in[to][from] = struct{}{}
	return nil
}

func (s *state) deleteEdge(from, to string) error {
	if _, ok := s.edges[Edge{From: from, To: to}]; !ok {
		return ErrNotFound
	}
	delete(s.edges, Edge{From: from, To: to})
	delete(s.out[from], to)
	delete(s.in[to], from)
	return nil
}

// reachable reports whether dst is reachable from src following
// directed edges. A node is reachable from itself.
func (s *state) reachable(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	queue := []string{src}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for next := range s.out[n] {
			if next == dst {
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

// Graph is a concurrency-safe in-memory directed acyclic graph.
// The zero value is not usable; construct it with New.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	st         *state
	generation uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{opts: o, st: newState()}, nil
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validateOp performs structural validation only; it never reads state.
func (g *Graph) validateOp(op Op) error {
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
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	cand := g.st.clone()
	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = cand.addNode(op.From)
		case DeleteNode:
			err = cand.deleteNode(op.From)
		case AddEdge:
			err = cand.addEdge(op.From, op.To)
		case DeleteEdge:
			err = cand.deleteEdge(op.From, op.To)
		}
		if err != nil {
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
