// Package trustgraph implements a concurrency-safe in-memory directed
// trust-dependency graph with atomic batches, cycle prevention and
// capacity limits enforced at batch end.
package trustgraph

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

// state is the mutable graph content. Graph never aliases it into results.
type state struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{} // from -> set of to
	in    map[string]map[string]struct{} // to -> set of from
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
	for from, tos := range s.out {
		m := make(map[string]struct{}, len(tos))
		for to := range tos {
			m[to] = struct{}{}
		}
		c.out[from] = m
	}
	for to, froms := range s.in {
		m := make(map[string]struct{}, len(froms))
		for from := range froms {
			m[from] = struct{}{}
		}
		c.in[to] = m
	}
	return c
}

func (s *state) addNode(n string) error {
	if _, ok := s.nodes[n]; ok {
		return ErrExists
	}
	s.nodes[n] = struct{}{}
	return nil
}

func (s *state) removeEdge(from, to string) {
	delete(s.edges, Edge{from, to})
	if m := s.out[from]; m != nil {
		delete(m, to)
		if len(m) == 0 {
			delete(s.out, from)
		}
	}
	if m := s.in[to]; m != nil {
		delete(m, from)
		if len(m) == 0 {
			delete(s.in, to)
		}
	}
}

func (s *state) deleteNode(n string) error {
	if _, ok := s.nodes[n]; !ok {
		return ErrNotFound
	}
	delete(s.nodes, n)
	for to := range s.out[n] {
		s.removeEdge(n, to)
	}
	for from := range s.in[n] {
		s.removeEdge(from, n)
	}
	return nil
}

// reachable reports whether to is reachable from from (from == to counts as
// reachable, provided both nodes exist).
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
	if _, ok := s.edges[Edge{from, to}]; !ok {
		return ErrNotFound
	}
	s.removeEdge(from, to)
	return nil
}

// Graph is a concurrency-safe in-memory trust dependency graph.
// The zero value is not usable; construct with New.
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

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
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
			if op.To != "" {
				return ErrInvalidInput
			}
			if !validName(op.From, g.maxNameLen) {
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

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.validate(b); err != nil {
		return Result{}, err
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

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.maxNameLen) || !validName(to, g.maxNameLen) {
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
