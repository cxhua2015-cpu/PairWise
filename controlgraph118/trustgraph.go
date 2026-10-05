package controlgraph118

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
}

func (s *state) clone() *state {
	c := &state{
		nodes: make(map[string]struct{}, len(s.nodes)),
		edges: make(map[Edge]struct{}, len(s.edges)),
		adj:   make(map[string]map[string]struct{}, len(s.adj)),
	}
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
	return c
}

// reachable reports whether target is reachable from source following
// directed edges. source == target is reachable (zero-length path).
func (s *state) reachable(source, target string) bool {
	if source == target {
		return true
	}
	seen := map[string]struct{}{source: {}}
	stack := []string{source}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range s.adj[cur] {
			if next == target {
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

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
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

// validateOp performs structural validation only; it must not read state.
func validateOp(op Op, maxNameBytes int) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" || !validName(op.From, maxNameBytes) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(op.From, maxNameBytes) || !validName(op.To, maxNameBytes) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

type Graph struct {
	mu         sync.RWMutex
	maxNodes   int
	maxEdges   int
	maxName    int
	st         *state
	generation uint64
}

func New(opts Options) (*Graph, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxNodes: opts.MaxNodes,
		maxEdges: opts.MaxEdges,
		maxName:  opts.MaxNameBytes,
		st: &state{
			nodes: map[string]struct{}{},
			edges: map[Edge]struct{}{},
			adj:   map[string]map[string]struct{}{},
		},
	}, nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := validateOp(op, g.maxName); err != nil {
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
	if len(cand.nodes) > g.maxNodes || len(cand.edges) > g.maxEdges {
		return Result{}, ErrCapacity
	}

	g.st = cand
	g.generation++
	return Result{Generation: g.generation}, nil
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
	delete(s.nodes, name)
	for to := range s.adj[name] {
		delete(s.edges, Edge{From: name, To: to})
	}
	delete(s.adj, name)
	for from, tos := range s.adj {
		if _, ok := tos[name]; ok {
			delete(tos, name)
			delete(s.edges, Edge{From: from, To: name})
			if len(tos) == 0 {
				delete(s.adj, from)
			}
		}
	}
	return nil
}

func (s *state) addEdge(from, to string) error {
	if _, ok := s.nodes[from]; !ok {
		return ErrNotFound
	}
	if _, ok := s.nodes[to]; !ok {
		return ErrNotFound
	}
	e := Edge{From: from, To: to}
	if _, ok := s.edges[e]; ok {
		return ErrExists
	}
	if s.reachable(to, from) {
		return ErrCycle
	}
	s.edges[e] = struct{}{}
	if s.adj[from] == nil {
		s.adj[from] = map[string]struct{}{}
	}
	s.adj[from][to] = struct{}{}
	return nil
}

func (s *state) deleteEdge(from, to string) error {
	e := Edge{From: from, To: to}
	if _, ok := s.edges[e]; !ok {
		return ErrNotFound
	}
	delete(s.edges, e)
	delete(s.adj[from], to)
	if len(s.adj[from]) == 0 {
		delete(s.adj, from)
	}
	return nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.maxName) || !validName(to, g.maxName) {
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
		Generation: g.generation,
		Nodes:      make([]string, 0, len(g.st.nodes)),
		Edges:      make([]Edge, 0, len(g.st.edges)),
	}
	for n := range g.st.nodes {
		snap.Nodes = append(snap.Nodes, n)
	}
	for e := range g.st.edges {
		snap.Edges = append(snap.Edges, e)
	}
	sort.Strings(snap.Nodes)
	sort.Slice(snap.Edges, func(i, j int) bool {
		if snap.Edges[i].From != snap.Edges[j].From {
			return snap.Edges[i].From < snap.Edges[j].From
		}
		return snap.Edges[i].To < snap.Edges[j].To
	})
	return snap
}
