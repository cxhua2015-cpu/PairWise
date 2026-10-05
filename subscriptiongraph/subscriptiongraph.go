package subscriptiongraph

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
	delete(s.out[e.From], e.To)
	if len(s.out[e.From]) == 0 {
		delete(s.out, e.From)
	}
	delete(s.in[e.To], e.From)
	if len(s.in[e.To]) == 0 {
		delete(s.in, e.To)
	}
}

// reachable reports whether target is reachable from start via >=0 edges.
func (s *state) reachable(start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	stack := []string{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range s.out[n] {
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

func validName(n string, max int) bool {
	if n == "" || len(n) > max {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (g *Graph) Apply(b Batch) (Result, error) {
	// Structural validation of the whole batch before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, g.maxNameLen) {
				return Result{}, ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.maxNameLen) || !validName(op.To, g.maxNameLen) {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
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
			for to := range cand.out[op.From] {
				cand.removeEdge(Edge{From: op.From, To: to})
			}
			for from := range cand.in[op.From] {
				cand.removeEdge(Edge{From: from, To: op.From})
			}
			delete(cand.nodes, op.From)
		case AddEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := cand.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.edges[e]; ok {
				return Result{}, ErrExists
			}
			if cand.reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			cand.addEdge(e)
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := cand.edges[e]; !ok {
				return Result{}, ErrNotFound
			}
			cand.removeEdge(e)
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
	s := Snapshot{Generation: g.generation}
	if len(g.st.nodes) > 0 {
		s.Nodes = make([]string, 0, len(g.st.nodes))
		for n := range g.st.nodes {
			s.Nodes = append(s.Nodes, n)
		}
		sort.Strings(s.Nodes)
	}
	if len(g.st.edges) > 0 {
		s.Edges = make([]Edge, 0, len(g.st.edges))
		for e := range g.st.edges {
			s.Edges = append(s.Edges, e)
		}
		sort.Slice(s.Edges, func(i, j int) bool {
			if s.Edges[i].From != s.Edges[j].From {
				return s.Edges[i].From < s.Edges[j].From
			}
			return s.Edges[i].To < s.Edges[j].To
		})
	}
	return s
}
