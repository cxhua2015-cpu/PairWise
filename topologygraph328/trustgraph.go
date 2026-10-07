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
}

func (s *state) clone() *state {
	c := &state{
		nodes: make(map[string]struct{}, len(s.nodes)),
		edges: make(map[Edge]struct{}, len(s.edges)),
		out:   make(map[string]map[string]struct{}, len(s.out)),
	}
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
	return c
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
		for next := range s.out[n] {
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

type Graph struct {
	mu  sync.RWMutex
	st  *state
	gen uint64
	opt Options
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		st: &state{
			nodes: map[string]struct{}{},
			edges: map[Edge]struct{}{},
			out:   map[string]map[string]struct{}{},
		},
		opt: o,
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

func (g *Graph) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, g.opt.MaxNameBytes) {
				return Result{}, ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.opt.MaxNameBytes) || !validName(op.To, g.opt.MaxNameBytes) {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}

	c := g.st.clone()
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := c.nodes[op.From]; ok {
				return Result{}, ErrExists
			}
			c.nodes[op.From] = struct{}{}
		case DeleteNode:
			if _, ok := c.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			delete(c.nodes, op.From)
			for to := range c.out[op.From] {
				delete(c.edges, Edge{op.From, to})
			}
			delete(c.out, op.From)
			for from, tos := range c.out {
				if _, ok := tos[op.From]; ok {
					delete(tos, op.From)
					delete(c.edges, Edge{from, op.From})
				}
			}
		case AddEdge:
			if _, ok := c.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := c.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			e := Edge{op.From, op.To}
			if _, ok := c.edges[e]; ok {
				return Result{}, ErrExists
			}
			if c.reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			c.edges[e] = struct{}{}
			if c.out[op.From] == nil {
				c.out[op.From] = map[string]struct{}{}
			}
			c.out[op.From][op.To] = struct{}{}
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := c.edges[e]; !ok {
				return Result{}, ErrNotFound
			}
			delete(c.edges, e)
			delete(c.out[op.From], op.To)
			if len(c.out[op.From]) == 0 {
				delete(c.out, op.From)
			}
		}
	}

	if len(c.nodes) > g.opt.MaxNodes || len(c.edges) > g.opt.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.st = c
	g.gen++
	return Result{Generation: g.gen}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.opt.MaxNameBytes) || !validName(to, g.opt.MaxNameBytes) {
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
	s := Snapshot{
		Generation: g.gen,
		Nodes:      make([]string, 0, len(g.st.nodes)),
		Edges:      make([]Edge, 0, len(g.st.edges)),
	}
	for n := range g.st.nodes {
		s.Nodes = append(s.Nodes, n)
	}
	sort.Strings(s.Nodes)
	for e := range g.st.edges {
		s.Edges = append(s.Edges, e)
	}
	sort.Slice(s.Edges, func(i, j int) bool {
		if s.Edges[i].From != s.Edges[j].From {
			return s.Edges[i].From < s.Edges[j].From
		}
		return s.Edges[i].To < s.Edges[j].To
	})
	return s
}
