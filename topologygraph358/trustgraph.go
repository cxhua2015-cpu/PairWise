package topologygraph358

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
	maxNameLen int
	nodes      map[string]struct{}
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
	edges      map[Edge]struct{}
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
		nodes:      make(map[string]struct{}),
		out:        make(map[string]map[string]struct{}),
		in:         make(map[string]map[string]struct{}),
		edges:      make(map[Edge]struct{}),
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

// validate checks the whole batch structurally before any state is read.
func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, g.maxNameLen) {
				return ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.maxNameLen) || !validName(op.To, g.maxNameLen) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// state is a candidate transaction view of the graph.
type state struct {
	nodes map[string]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
	edges map[Edge]struct{}
}

func (g *Graph) clone() *state {
	s := &state{
		nodes: make(map[string]struct{}, len(g.nodes)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
		in:    make(map[string]map[string]struct{}, len(g.in)),
		edges: make(map[Edge]struct{}, len(g.edges)),
	}
	for n := range g.nodes {
		s.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		s.edges[e] = struct{}{}
	}
	for n, m := range g.out {
		nm := make(map[string]struct{}, len(m))
		for t := range m {
			nm[t] = struct{}{}
		}
		s.out[n] = nm
	}
	for n, m := range g.in {
		nm := make(map[string]struct{}, len(m))
		for t := range m {
			nm[t] = struct{}{}
		}
		s.in[n] = nm
	}
	return s
}

func (s *state) reachable(from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range s.out[n] {
			if m == to {
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

func (s *state) addNode(n string) error {
	if _, ok := s.nodes[n]; ok {
		return ErrExists
	}
	s.nodes[n] = struct{}{}
	return nil
}

func (s *state) deleteNode(n string) error {
	if _, ok := s.nodes[n]; !ok {
		return ErrNotFound
	}
	for t := range s.out[n] {
		delete(s.edges, Edge{n, t})
		delete(s.in[t], n)
	}
	for f := range s.in[n] {
		delete(s.edges, Edge{f, n})
		delete(s.out[f], n)
	}
	delete(s.out, n)
	delete(s.in, n)
	delete(s.nodes, n)
	return nil
}

func (s *state) addEdge(from, to string) error {
	if _, ok := s.nodes[from]; !ok {
		return ErrNotFound
	}
	if _, ok := s.nodes[to]; !ok {
		return ErrNotFound
	}
	e := Edge{from, to}
	if _, ok := s.edges[e]; ok {
		return ErrExists
	}
	if s.reachable(to, from) {
		return ErrCycle
	}
	s.edges[e] = struct{}{}
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
	e := Edge{from, to}
	if _, ok := s.edges[e]; !ok {
		return ErrNotFound
	}
	delete(s.edges, e)
	delete(s.out[from], to)
	delete(s.in[to], from)
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.validate(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	s := g.clone()
	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = s.addNode(op.From)
		case DeleteNode:
			err = s.deleteNode(op.From)
		case AddEdge:
			err = s.addEdge(op.From, op.To)
		case DeleteEdge:
			err = s.deleteEdge(op.From, op.To)
		}
		if err != nil {
			return Result{}, err
		}
	}
	if len(s.nodes) > g.maxNodes || len(s.edges) > g.maxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.out, g.in, g.edges = s.nodes, s.out, s.in, s.edges
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.maxNameLen) || !validName(to, g.maxNameLen) {
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
	return s.reachable(from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	snap := Snapshot{
		Generation: g.generation,
		Nodes:      make([]string, 0, len(g.nodes)),
		Edges:      make([]Edge, 0, len(g.edges)),
	}
	for n := range g.nodes {
		snap.Nodes = append(snap.Nodes, n)
	}
	sort.Strings(snap.Nodes)
	for e := range g.edges {
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
