package topologygraph288

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

// Graph is a concurrency-safe in-memory control topology graph.
// All state is guarded by a single RWMutex; mutations are staged in a
// candidate transaction and swapped in atomically on success.
type Graph struct {
	mu    sync.RWMutex
	opts  Options
	gen   uint64
	nodes map[string]struct{}
	edges map[Edge]struct{}
	adj   map[string]map[string]struct{}
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  o,
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		adj:   make(map[string]map[string]struct{}),
	}, nil
}

// Apply validates the whole batch structurally, stages it on a candidate
// copy of the state, checks final capacity, and commits atomically.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.validateStructural(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}
	c := g.candidateLocked()
	for _, op := range b.Ops {
		if err := c.applyOp(op); err != nil {
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

// candidateLocked builds a deep copy of the committed state to serve as the
// candidate transaction. Callers must hold the lock.
func (g *Graph) candidateLocked() *Graph {
	c := &Graph{
		opts:  g.opts,
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
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

// applyOp applies a single op to the (candidate) state. It is not
// concurrency-safe and is only used on unshared candidate copies.
func (g *Graph) applyOp(op Op) error {
	switch op.Kind {
	case AddNode:
		if _, ok := g.nodes[op.From]; ok {
			return ErrExists
		}
		g.nodes[op.From] = struct{}{}
	case DeleteNode:
		if _, ok := g.nodes[op.From]; !ok {
			return ErrNotFound
		}
		delete(g.nodes, op.From)
		for to := range g.adj[op.From] {
			delete(g.edges, Edge{From: op.From, To: to})
		}
		delete(g.adj, op.From)
		for from, tos := range g.adj {
			if _, ok := tos[op.From]; ok {
				delete(tos, op.From)
				delete(g.edges, Edge{From: from, To: op.From})
			}
		}
	case AddEdge:
		if _, ok := g.nodes[op.From]; !ok {
			return ErrNotFound
		}
		if _, ok := g.nodes[op.To]; !ok {
			return ErrNotFound
		}
		e := Edge{From: op.From, To: op.To}
		if _, ok := g.edges[e]; ok {
			return ErrExists
		}
		if g.reachableLocked(op.To, op.From) {
			return ErrCycle
		}
		g.edges[e] = struct{}{}
		if g.adj[op.From] == nil {
			g.adj[op.From] = make(map[string]struct{})
		}
		g.adj[op.From][op.To] = struct{}{}
	case DeleteEdge:
		e := Edge{From: op.From, To: op.To}
		if _, ok := g.edges[e]; !ok {
			return ErrNotFound
		}
		delete(g.edges, e)
		delete(g.adj[op.From], op.To)
		if len(g.adj[op.From]) == 0 {
			delete(g.adj, op.From)
		}
	}
	return nil
}

// reachableLocked reports whether dst is reachable from src following
// directed edges. Callers must hold the lock or use an unshared candidate.
func (g *Graph) reachableLocked(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for to := range g.adj[n] {
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

func (g *Graph) Reachable(from, to string) (bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return false, ErrNotFound
	}
	return g.reachableLocked(from, to), nil
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
