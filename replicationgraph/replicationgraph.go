// Package replicationgraph implements a concurrency-safe, in-memory
// directed replication topology graph with atomic batch mutations.
package replicationgraph

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

// Graph is a concurrency-safe in-memory replication topology graph.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{} // adjacency: from -> to set
	generation uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  o,
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		out:   make(map[string]map[string]struct{}),
	}, nil
}

// validName reports whether s is a non-empty ASCII name of lowercase
// letters, digits, hyphens and underscores within the byte limit.
func (g *Graph) validName(s string) bool {
	if s == "" || len(s) > g.opts.MaxNameBytes {
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

// validateOp structurally validates a single op without reading state.
func (g *Graph) validateOp(op Op) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" || !g.validName(op.From) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !g.validName(op.From) || !g.validName(op.To) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// candidate is a mutable working copy of the graph state used to apply a
// batch transactionally; it is discarded on failure.
type candidate struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
}

func (g *Graph) fork() *candidate {
	c := &candidate{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, set := range g.out {
		ns := make(map[string]struct{}, len(set))
		for to := range set {
			ns[to] = struct{}{}
		}
		c.out[from] = ns
	}
	return c
}

// reachable reports whether dst is reachable from src via out-edges.
func (c *candidate) reachable(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for to := range c.out[n] {
			if to == dst {
				return true
			}
			if _, ok := seen[to]; !ok {
				seen[to] = struct{}{}
				stack = append(stack, to)
			}
		}
	}
	return false
}

func (c *candidate) addEdge(from, to string) {
	c.edges[Edge{from, to}] = struct{}{}
	set, ok := c.out[from]
	if !ok {
		set = make(map[string]struct{})
		c.out[from] = set
	}
	set[to] = struct{}{}
}

func (c *candidate) deleteEdge(from, to string) {
	delete(c.edges, Edge{from, to})
	if set, ok := c.out[from]; ok {
		delete(set, to)
		if len(set) == 0 {
			delete(c.out, from)
		}
	}
}

func (c *candidate) deleteNode(n string) {
	delete(c.nodes, n)
	// Remove out-edges of n.
	if set, ok := c.out[n]; ok {
		for to := range set {
			delete(c.edges, Edge{n, to})
		}
		delete(c.out, n)
	}
	// Remove in-edges of n.
	for from, set := range c.out {
		if _, ok := set[n]; ok {
			delete(c.edges, Edge{from, n})
			delete(set, n)
			if len(set) == 0 {
				delete(c.out, from)
			}
		}
	}
}

// Apply validates and atomically applies a batch of ops. On any error the
// graph is left unchanged. Capacity limits are checked only at batch end.
func (g *Graph) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before reading any state.
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		gen := g.generation
		g.mu.RUnlock()
		return Result{Generation: gen}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	c := g.fork()
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
			c.deleteNode(op.From)
		case AddEdge:
			if _, ok := c.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := c.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := c.edges[Edge{op.From, op.To}]; ok {
				return Result{}, ErrExists
			}
			if c.reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			c.addEdge(op.From, op.To)
		case DeleteEdge:
			if _, ok := c.edges[Edge{op.From, op.To}]; !ok {
				return Result{}, ErrNotFound
			}
			c.deleteEdge(op.From, op.To)
		}
	}
	if len(c.nodes) > g.opts.MaxNodes || len(c.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes = c.nodes
	g.edges = c.edges
	g.out = c.out
	g.generation++
	return Result{Generation: g.generation}, nil
}

// Reachable reports whether dst is reachable from dst via directed edges,
// using a consistent snapshot of the current state.
func (g *Graph) Reachable(src, dst string) (bool, error) {
	if !g.validName(src) || !g.validName(dst) {
		return false, ErrInvalidInput
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[src]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[dst]; !ok {
		return false, ErrNotFound
	}
	c := &candidate{out: g.out}
	return c.reachable(src, dst), nil
}

// Snapshot returns a consistently ordered, detached copy of the graph.
func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := Snapshot{
		Generation: g.generation,
		Nodes:      make([]string, 0, len(g.nodes)),
		Edges:      make([]Edge, 0, len(g.edges)),
	}
	for n := range g.nodes {
		s.Nodes = append(s.Nodes, n)
	}
	for e := range g.edges {
		s.Edges = append(s.Edges, e)
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
