package topologygraph223

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
//
// Indexes: nodes and edges are kept in hash maps for O(1) membership;
// adjacency (from -> set of to) accelerates cycle checks and Reachable.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	adj        map[string]map[string]struct{}
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
		adj:   make(map[string]map[string]struct{}),
	}, nil
}

// Apply validates the batch structurally, replays it against a candidate
// transaction, checks final capacity at the end, and commits atomically.
// Any failure rolls the whole batch back.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	cand := g.candidateLocked()
	for _, op := range b.Ops {
		if err := cand.applyOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(cand.nodes) > g.opts.MaxNodes || len(cand.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	if len(b.Ops) > 0 {
		g.nodes, g.edges, g.adj = cand.nodes, cand.edges, cand.adj
		g.generation++
	}
	return Result{Generation: g.generation}, nil
}

// candidateLocked snapshots the mutable state for a candidate transaction.
// Callers must hold g.mu.
func (g *Graph) candidateLocked() *candidate {
	c := &candidate{
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

// candidate is a private transactional view; it is discarded on failure.
type candidate struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	adj   map[string]map[string]struct{}
}

func (c *candidate) applyOp(op Op) error {
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
			if e.From == op.From || e.To == op.From {
				delete(c.edges, e)
			}
		}
		delete(c.adj, op.From)
		for from, tos := range c.adj {
			delete(tos, op.From)
			if len(tos) == 0 {
				delete(c.adj, from)
			}
		}
	case AddEdge:
		e := Edge{From: op.From, To: op.To}
		if _, ok := c.nodes[op.From]; !ok {
			return ErrNotFound
		}
		if _, ok := c.nodes[op.To]; !ok {
			return ErrNotFound
		}
		if _, ok := c.edges[e]; ok {
			return ErrExists
		}
		if c.reachable(op.To, op.From) {
			return ErrCycle
		}
		c.edges[e] = struct{}{}
		if c.adj[op.From] == nil {
			c.adj[op.From] = make(map[string]struct{})
		}
		c.adj[op.From][op.To] = struct{}{}
	case DeleteEdge:
		e := Edge{From: op.From, To: op.To}
		if _, ok := c.edges[e]; !ok {
			return ErrNotFound
		}
		delete(c.edges, e)
		delete(c.adj[op.From], op.To)
		if len(c.adj[op.From]) == 0 {
			delete(c.adj, op.From)
		}
	}
	return nil
}

// reachable reports whether target is reachable from start (BFS over adj).
func (c *candidate) reachable(start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for next := range c.adj[cur] {
			if next == target {
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

// Reachable reports whether dst is reachable from src in the current
// consistent snapshot of the graph.
func (g *Graph) Reachable(src, dst string) (bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[src]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[dst]; !ok {
		return false, ErrNotFound
	}
	c := &candidate{adj: g.adj}
	return c.reachable(src, dst), nil
}

// Snapshot returns a stably sorted, fully owned view of the graph.
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
