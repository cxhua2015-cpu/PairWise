package topologygraph308

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
	adj   map[string]map[string]struct{}
	rev   map[string]map[string]struct{}
}

func newState() *state {
	return &state{
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		adj:   make(map[string]map[string]struct{}),
		rev:   make(map[string]map[string]struct{}),
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
	for from, tos := range s.adj {
		m := make(map[string]struct{}, len(tos))
		for to := range tos {
			m[to] = struct{}{}
		}
		c.adj[from] = m
	}
	for to, froms := range s.rev {
		m := make(map[string]struct{}, len(froms))
		for from := range froms {
			m[from] = struct{}{}
		}
		c.rev[to] = m
	}
	return c
}

func (s *state) addEdge(from, to string) {
	s.edges[Edge{From: from, To: to}] = struct{}{}
	if s.adj[from] == nil {
		s.adj[from] = make(map[string]struct{})
	}
	s.adj[from][to] = struct{}{}
	if s.rev[to] == nil {
		s.rev[to] = make(map[string]struct{})
	}
	s.rev[to][from] = struct{}{}
}

func (s *state) removeEdge(from, to string) {
	delete(s.edges, Edge{From: from, To: to})
	if tos := s.adj[from]; tos != nil {
		delete(tos, to)
		if len(tos) == 0 {
			delete(s.adj, from)
		}
	}
	if froms := s.rev[to]; froms != nil {
		delete(froms, from)
		if len(froms) == 0 {
			delete(s.rev, to)
		}
	}
}

func (s *state) removeNode(name string) {
	for to := range s.adj[name] {
		s.removeEdge(name, to)
	}
	for from := range s.rev[name] {
		s.removeEdge(from, name)
	}
	delete(s.nodes, name)
}

// reachable reports whether dst is reachable from src following adj.
func (s *state) reachable(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	queue := []string{src}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for next := range s.adj[cur] {
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

type Graph struct {
	mu         sync.RWMutex
	maxNodes   int
	maxEdges   int
	maxNameLen int
	st         *state
	generation uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxNodes:   o.MaxNodes,
		maxEdges:   o.MaxEdges,
		maxNameLen: o.MaxNameBytes,
		st:         newState(),
	}, nil
}

func validName(name string, maxLen int) bool {
	if name == "" || len(name) > maxLen {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validateOp checks the structural shape of an op without reading state.
func validateOp(op Op, maxNameLen int) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" {
			return ErrInvalidInput
		}
		if !validName(op.From, maxNameLen) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(op.From, maxNameLen) || !validName(op.To, maxNameLen) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := validateOp(op, g.maxNameLen); err != nil {
			return Result{}, err
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
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
			cand.removeNode(op.From)
		case AddEdge:
			if _, ok := cand.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			e := Edge{From: op.From, To: op.To}
			if _, ok := cand.edges[e]; ok {
				return Result{}, ErrExists
			}
			if cand.reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			cand.addEdge(op.From, op.To)
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := cand.edges[e]; !ok {
				return Result{}, ErrNotFound
			}
			cand.removeEdge(op.From, op.To)
		}
	}

	if len(cand.nodes) > g.maxNodes || len(cand.edges) > g.maxEdges {
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
