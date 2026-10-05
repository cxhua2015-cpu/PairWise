package controlgraph093

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
	mu         sync.RWMutex
	maxNodes   int
	maxEdges   int
	maxName    int
	generation uint64
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxNodes: o.MaxNodes,
		maxEdges: o.MaxEdges,
		maxName:  o.MaxNameBytes,
		nodes:    make(map[string]struct{}),
		edges:    make(map[Edge]struct{}),
		out:      make(map[string]map[string]struct{}),
		in:       make(map[string]map[string]struct{}),
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

// validate checks structural constraints only; it must not read graph state.
func (g *Graph) validate(op Op) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" || !validName(op.From, g.maxName) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(op.From, g.maxName) || !validName(op.To, g.maxName) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

type state struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

func (g *Graph) cloneState() *state {
	s := &state{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
		in:    make(map[string]map[string]struct{}, len(g.in)),
	}
	for k := range g.nodes {
		s.nodes[k] = struct{}{}
	}
	for k := range g.edges {
		s.edges[k] = struct{}{}
	}
	for k, v := range g.out {
		m := make(map[string]struct{}, len(v))
		for x := range v {
			m[x] = struct{}{}
		}
		s.out[k] = m
	}
	for k, v := range g.in {
		m := make(map[string]struct{}, len(v))
		for x := range v {
			m[x] = struct{}{}
		}
		s.in[k] = m
	}
	return s
}

func (s *state) addEdge(from, to string) {
	s.edges[Edge{from, to}] = struct{}{}
	if s.out[from] == nil {
		s.out[from] = make(map[string]struct{})
	}
	s.out[from][to] = struct{}{}
	if s.in[to] == nil {
		s.in[to] = make(map[string]struct{})
	}
	s.in[to][from] = struct{}{}
}

func (s *state) delEdge(from, to string) {
	delete(s.edges, Edge{from, to})
	delete(s.out[from], to)
	delete(s.in[to], from)
}

// reaches reports whether target is reachable from start following out-edges.
func (s *state) reaches(start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	stack := []string{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range s.out[n] {
			if m == target {
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

func (g *Graph) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := g.validate(op); err != nil {
			return Result{}, err
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	s := g.cloneState()
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := s.nodes[op.From]; ok {
				return Result{}, ErrExists
			}
			s.nodes[op.From] = struct{}{}
		case DeleteNode:
			if _, ok := s.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			delete(s.nodes, op.From)
			for to := range s.out[op.From] {
				s.delEdge(op.From, to)
			}
			for from := range s.in[op.From] {
				s.delEdge(from, op.From)
			}
			delete(s.out, op.From)
			delete(s.in, op.From)
		case AddEdge:
			if _, ok := s.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := s.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := s.edges[Edge{op.From, op.To}]; ok {
				return Result{}, ErrExists
			}
			if s.reaches(op.To, op.From) {
				return Result{}, ErrCycle
			}
			s.addEdge(op.From, op.To)
		case DeleteEdge:
			if _, ok := s.edges[Edge{op.From, op.To}]; !ok {
				return Result{}, ErrNotFound
			}
			s.delEdge(op.From, op.To)
		}
	}
	if len(s.nodes) > g.maxNodes || len(s.edges) > g.maxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes = s.nodes
	g.edges = s.edges
	g.out = s.out
	g.in = s.in
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.maxName) || !validName(to, g.maxName) {
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
	s := &state{out: g.out}
	return s.reaches(from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	nodes := make([]string, 0, len(g.nodes))
	for n := range g.nodes {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	edges := make([]Edge, 0, len(g.edges))
	for e := range g.edges {
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
