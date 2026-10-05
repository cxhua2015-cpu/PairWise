package controlgraph098

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
		nodes: map[string]struct{}{},
		edges: map[Edge]struct{}{},
		out:   map[string]map[string]struct{}{},
		in:    map[string]map[string]struct{}{},
	}, nil
}

func validName(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (g *Graph) validate(op Op) error {
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

type state struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

func (g *Graph) clone() *state {
	s := &state{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
		in:    make(map[string]map[string]struct{}, len(g.in)),
	}
	for n := range g.nodes {
		s.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		s.edges[e] = struct{}{}
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
		s.out[from] = map[string]struct{}{}
	}
	s.out[from][to] = struct{}{}
	if s.in[to] == nil {
		s.in[to] = map[string]struct{}{}
	}
	s.in[to][from] = struct{}{}
}

func (s *state) removeEdge(from, to string) {
	delete(s.edges, Edge{from, to})
	delete(s.out[from], to)
	if len(s.out[from]) == 0 {
		delete(s.out, from)
	}
	delete(s.in[to], from)
	if len(s.in[to]) == 0 {
		delete(s.in, to)
	}
}

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

func (g *Graph) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := g.validate(op); err != nil {
			return Result{}, err
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}
	s := g.clone()
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
			for to := range s.out[op.From] {
				s.removeEdge(op.From, to)
			}
			for from := range s.in[op.From] {
				s.removeEdge(from, op.From)
			}
			delete(s.nodes, op.From)
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
			if s.reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			s.addEdge(op.From, op.To)
		case DeleteEdge:
			if _, ok := s.edges[Edge{op.From, op.To}]; !ok {
				return Result{}, ErrNotFound
			}
			s.removeEdge(op.From, op.To)
		}
	}
	if len(s.nodes) > g.opts.MaxNodes || len(s.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges, g.out, g.in = s.nodes, s.edges, s.out, s.in
	g.gen++
	return Result{Generation: g.gen}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return false, ErrNotFound
	}
	s := &state{out: g.out}
	return s.reachable(from, to), nil
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
	return Snapshot{Generation: g.gen, Nodes: nodes, Edges: edges}
}
