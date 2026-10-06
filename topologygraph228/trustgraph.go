package topologygraph228

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
//
// Indexes: nodes keeps the node set, edges keeps the edge set keyed by
// "from\x00to", and out maps each node to its outgoing neighbors so cycle
// detection and reachability run without scanning the whole edge set.
type Graph struct {
	mu    sync.RWMutex
	opts  Options
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
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
		out:   make(map[string]map[string]struct{}),
	}, nil
}

// Apply executes a batch atomically. The batch is fully validated
// structurally before any state is read, then applied to a candidate
// transaction state; capacity limits are checked only at the end and any
// failure rolls the whole batch back.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		gen := g.gen
		g.mu.RUnlock()
		return Result{Generation: gen}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	cand := g.candidateLocked()
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
	if len(cand.nodes) > g.opts.MaxNodes || len(cand.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes, g.edges, g.out = cand.nodes, cand.edges, cand.out
	g.gen++
	return Result{Generation: g.gen}, nil
}

// Reachable reports whether dst is reachable from src using a consistent
// snapshot of the current state.
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
	return reachable(g.out, src, dst), nil
}

// Snapshot returns a consistently ordered, state-isolated view.
func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := Snapshot{Generation: g.gen, Nodes: make([]string, 0, len(g.nodes)), Edges: make([]Edge, 0, len(g.edges))}
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

// candidateLocked snapshots the mutable indexes into a candidate transaction
// state. Callers must hold g.mu.
func (g *Graph) candidateLocked() *candidate {
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
	for from, tos := range g.out {
		m := make(map[string]struct{}, len(tos))
		for to := range tos {
			m[to] = struct{}{}
		}
		c.out[from] = m
	}
	return c
}

// candidate is the speculative transaction state mutated by Apply. It is
// discarded on failure, giving rollback semantics, and swapped in on success.
type candidate struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
}

func (c *candidate) addNode(n string) error {
	if _, ok := c.nodes[n]; ok {
		return ErrExists
	}
	c.nodes[n] = struct{}{}
	return nil
}

func (c *candidate) deleteNode(n string) error {
	if _, ok := c.nodes[n]; !ok {
		return ErrNotFound
	}
	delete(c.nodes, n)
	for to := range c.out[n] {
		delete(c.edges, Edge{From: n, To: to})
	}
	delete(c.out, n)
	for from, tos := range c.out {
		if _, ok := tos[n]; ok {
			delete(tos, n)
			delete(c.edges, Edge{From: from, To: n})
		}
	}
	return nil
}

func (c *candidate) addEdge(from, to string) error {
	if _, ok := c.nodes[from]; !ok {
		return ErrNotFound
	}
	if _, ok := c.nodes[to]; !ok {
		return ErrNotFound
	}
	e := Edge{From: from, To: to}
	if _, ok := c.edges[e]; ok {
		return ErrExists
	}
	if reachable(c.out, to, from) {
		return ErrCycle
	}
	c.edges[e] = struct{}{}
	m, ok := c.out[from]
	if !ok {
		m = make(map[string]struct{})
		c.out[from] = m
	}
	m[to] = struct{}{}
	return nil
}

func (c *candidate) deleteEdge(from, to string) error {
	e := Edge{From: from, To: to}
	if _, ok := c.edges[e]; !ok {
		return ErrNotFound
	}
	delete(c.edges, e)
	if m, ok := c.out[from]; ok {
		delete(m, to)
		if len(m) == 0 {
			delete(c.out, from)
		}
	}
	return nil
}

// reachable reports whether dst is reachable from src over the adjacency index.
func reachable(out map[string]map[string]struct{}, src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range out[n] {
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
