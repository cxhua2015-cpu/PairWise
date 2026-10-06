package topologygraph263

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

// Graph is a concurrency-safe in-memory directed control-topology graph.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
	generation uint64
}

func New(opts Options) (*Graph, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  opts,
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		out:   make(map[string]map[string]struct{}),
		in:    make(map[string]map[string]struct{}),
	}, nil
}

// Apply validates the whole batch structurally, then applies it against a
// candidate copy of the state. Any failure rolls the batch back atomically.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	cand := g.candidate()
	for _, op := range b.Ops {
		if err := cand.applyOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(cand.nodes) > g.opts.MaxNodes || len(cand.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges, g.out, g.in = cand.nodes, cand.edges, cand.out, cand.in
	g.generation++
	return Result{Generation: g.generation}, nil
}

// candidate returns a mutable working copy of the committed state.
func (g *Graph) candidate() *Graph {
	c := &Graph{
		opts:  g.opts,
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
		in:    make(map[string]map[string]struct{}, len(g.in)),
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
	for to, set := range g.in {
		ns := make(map[string]struct{}, len(set))
		for from := range set {
			ns[from] = struct{}{}
		}
		c.in[to] = ns
	}
	return c
}

// applyOp mutates the candidate state for a single op. The caller holds no
// lock; candidates are private to one Apply call.
func (c *Graph) applyOp(op Op) error {
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
		for to := range c.out[op.From] {
			delete(c.edges, Edge{op.From, to})
			delete(c.in[to], op.From)
		}
		delete(c.out, op.From)
		for from := range c.in[op.From] {
			delete(c.edges, Edge{from, op.From})
			delete(c.out[from], op.From)
		}
		delete(c.in, op.From)
	case AddEdge:
		if _, ok := c.nodes[op.From]; !ok {
			return ErrNotFound
		}
		if _, ok := c.nodes[op.To]; !ok {
			return ErrNotFound
		}
		e := Edge{op.From, op.To}
		if _, ok := c.edges[e]; ok {
			return ErrExists
		}
		if c.reachableLocked(op.To, op.From) {
			return ErrCycle
		}
		c.edges[e] = struct{}{}
		if c.out[op.From] == nil {
			c.out[op.From] = make(map[string]struct{})
		}
		c.out[op.From][op.To] = struct{}{}
		if c.in[op.To] == nil {
			c.in[op.To] = make(map[string]struct{})
		}
		c.in[op.To][op.From] = struct{}{}
	case DeleteEdge:
		e := Edge{op.From, op.To}
		if _, ok := c.edges[e]; !ok {
			return ErrNotFound
		}
		delete(c.edges, e)
		delete(c.out[op.From], op.To)
		delete(c.in[op.To], op.From)
	}
	return nil
}

// reachableLocked reports whether target is reachable from start. Callers must
// hold g.mu (or operate on a private candidate).
func (g *Graph) reachableLocked(start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	queue := []string{start}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for next := range g.out[n] {
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

func (g *Graph) Reachable(from, to string) (bool, error) {
	if err := g.validName(from); err != nil {
		return false, err
	}
	if err := g.validName(to); err != nil {
		return false, err
	}
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
	nodes := make([]string, 0, len(g.nodes))
	for n := range g.nodes {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	edges := make([]Edge, 0, len(g.edges))
	for e := range g.edges {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return Snapshot{Generation: g.generation, Nodes: nodes, Edges: edges}
}
