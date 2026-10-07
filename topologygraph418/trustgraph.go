package topologygraph418

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

// Graph is a concurrency-safe in-memory directed topology graph.
// All state transitions happen through atomic batches guarded by mu.
type Graph struct {
	mu    sync.RWMutex
	opts  Options
	nodes map[string]struct{}
	edges map[Edge]struct{}
	adj   map[string]map[string]struct{} // forward adjacency index
	rev   map[string]map[string]struct{} // reverse adjacency index
	gen   uint64
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
		rev:   make(map[string]map[string]struct{}),
	}, nil
}

// Apply validates the batch structurally, then applies every op against a
// candidate copy of the state. Capacity limits are checked only on the final
// candidate state; any failure rolls the whole batch back.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
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
	g.nodes, g.edges, g.adj, g.rev = c.nodes, c.edges, c.adj, c.rev
	g.gen++
	return Result{Generation: g.gen}, nil
}

// candidateLocked clones the live state into a candidate transaction view.
// Caller must hold the lock.
func (g *Graph) candidateLocked() *Graph {
	c := &Graph{
		opts:  g.opts,
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		adj:   make(map[string]map[string]struct{}, len(g.adj)),
		rev:   make(map[string]map[string]struct{}, len(g.rev)),
		gen:   g.gen,
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, set := range g.adj {
		ns := make(map[string]struct{}, len(set))
		for to := range set {
			ns[to] = struct{}{}
		}
		c.adj[from] = ns
	}
	for to, set := range g.rev {
		ns := make(map[string]struct{}, len(set))
		for from := range set {
			ns[from] = struct{}{}
		}
		c.rev[to] = ns
	}
	return c
}

// applyOp mutates the candidate state for a single op. The op is assumed to
// be structurally valid already.
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
		for to := range g.adj[op.From] {
			g.removeEdge(op.From, to)
		}
		for from := range g.rev[op.From] {
			g.removeEdge(from, op.From)
		}
		delete(g.nodes, op.From)
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
		if g.rev[op.To] == nil {
			g.rev[op.To] = make(map[string]struct{})
		}
		g.rev[op.To][op.From] = struct{}{}
	case DeleteEdge:
		e := Edge{From: op.From, To: op.To}
		if _, ok := g.edges[e]; !ok {
			return ErrNotFound
		}
		g.removeEdge(op.From, op.To)
	}
	return nil
}

func (g *Graph) removeEdge(from, to string) {
	delete(g.edges, Edge{From: from, To: to})
	delete(g.adj[from], to)
	if len(g.adj[from]) == 0 {
		delete(g.adj, from)
	}
	delete(g.rev[to], from)
	if len(g.rev[to]) == 0 {
		delete(g.rev, to)
	}
}

// reachableLocked reports whether dst is reachable from src (src == dst
// counts as reachable). Caller must hold the lock or work on a candidate.
func (g *Graph) reachableLocked(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range g.adj[n] {
			if next == dst {
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

// Reachable reports whether a directed path from src to dst exists, using a
// consistent snapshot of the current state.
func (g *Graph) Reachable(src, dst string) (bool, error) {
	if !validName(src, g.opts.MaxNameBytes) || !validName(dst, g.opts.MaxNameBytes) {
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
	return g.reachableLocked(src, dst), nil
}

// Snapshot returns a stably sorted, fully detached view of the state.
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
