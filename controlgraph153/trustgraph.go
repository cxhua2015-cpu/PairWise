package controlgraph153

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

type edgeKey struct{ from, to string }

type Graph struct {
	mu    sync.RWMutex
	opts  Options
	nodes map[string]struct{}
	edges map[edgeKey]struct{}
	adj   map[string]map[string]struct{}
	gen   uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  o,
		nodes: make(map[string]struct{}),
		edges: make(map[edgeKey]struct{}),
		adj:   make(map[string]map[string]struct{}),
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

// validateOp performs structural validation only; it must not read state.
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

// candidate is a mutable working copy of the graph state.
type candidate struct {
	nodes map[string]struct{}
	edges map[edgeKey]struct{}
	adj   map[string]map[string]struct{}
}

func (g *Graph) clone() *candidate {
	c := &candidate{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[edgeKey]struct{}, len(g.edges)),
		adj:   make(map[string]map[string]struct{}, len(g.adj)),
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, tos := range g.adj {
		m := make(map[string]struct{}, len(tos))
		for to := range tos {
			m[to] = struct{}{}
		}
		c.adj[from] = m
	}
	return c
}

func (c *candidate) reachable(from, to string) bool {
	if from == to {
		_, ok := c.nodes[from]
		return ok
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range c.adj[n] {
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

func (c *candidate) apply(op Op) error {
	switch op.Kind {
	case AddNode:
		if _, ok := c.nodes[op.From]; ok {
			return ErrExists
		}
		c.nodes[op.From] = struct{}{}
	case DeleteNode:
		if _, ok := c.nodes[op.From]; !ok {
			return ErrNotFound
		}
		delete(c.nodes, op.From)
		for e := range c.edges {
			if e.from == op.From || e.to == op.From {
				delete(c.edges, e)
				delete(c.adj[e.from], e.to)
			}
		}
		delete(c.adj, op.From)
	case AddEdge:
		if _, ok := c.nodes[op.From]; !ok {
			return ErrNotFound
		}
		if _, ok := c.nodes[op.To]; !ok {
			return ErrNotFound
		}
		k := edgeKey{op.From, op.To}
		if _, ok := c.edges[k]; ok {
			return ErrExists
		}
		if c.reachable(op.To, op.From) {
			return ErrCycle
		}
		c.edges[k] = struct{}{}
		if c.adj[op.From] == nil {
			c.adj[op.From] = make(map[string]struct{})
		}
		c.adj[op.From][op.To] = struct{}{}
	case DeleteEdge:
		k := edgeKey{op.From, op.To}
		if _, ok := c.edges[k]; !ok {
			return ErrNotFound
		}
		delete(c.edges, k)
		delete(c.adj[op.From], op.To)
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		gen := g.gen
		g.mu.RUnlock()
		return Result{Generation: gen}, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	c := g.clone()
	for _, op := range b.Ops {
		if err := c.apply(op); err != nil {
			return Result{}, err
		}
	}
	if len(c.nodes) > g.opts.MaxNodes || len(c.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges, g.adj = c.nodes, c.edges, c.adj
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
	c := &candidate{nodes: g.nodes, adj: g.adj}
	return c.reachable(from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := Snapshot{
		Generation: g.gen,
		Nodes:      make([]string, 0, len(g.nodes)),
		Edges:      make([]Edge, 0, len(g.edges)),
	}
	for n := range g.nodes {
		s.Nodes = append(s.Nodes, n)
	}
	for e := range g.edges {
		s.Edges = append(s.Edges, Edge{From: e.from, To: e.to})
	}
	sort.Strings(s.Nodes)
	sort.Slice(s.Edges, func(i, j int) bool {
		if s.Edges[i].From != s.Edges[j].From {
			return s.Edges[i].From < s.Edges[j].From
		}
		return s.Edges[i].To < s.Edges[j].To
	})
	return s
}
