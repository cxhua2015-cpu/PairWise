package topologygraph278

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
type Graph struct {
	mu    sync.RWMutex
	opts  Options
	gen   uint64
	nodes map[string]struct{}
	edges map[Edge]struct{}
	adj   map[string]map[string]struct{} // from -> set of to
	rev   map[string]map[string]struct{} // to -> set of from
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

// Apply validates the batch structurally, then applies it atomically.
// Capacity limits are checked only against the final state; any failure
// rolls the whole batch back.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}
	c := g.candidate()
	for _, op := range b.Ops {
		if err := c.applyOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(c.nodes) > g.opts.MaxNodes || len(c.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.commit(c)
	g.gen++
	return Result{Generation: g.gen}, nil
}

// candidate is a scratch copy of the state used for speculative execution.
type candidate struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	adj   map[string]map[string]struct{}
	rev   map[string]map[string]struct{}
}

func (g *Graph) candidate() *candidate {
	c := &candidate{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		adj:   make(map[string]map[string]struct{}, len(g.adj)),
		rev:   make(map[string]map[string]struct{}, len(g.rev)),
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, tos := range g.adj {
		s := make(map[string]struct{}, len(tos))
		for to := range tos {
			s[to] = struct{}{}
		}
		c.adj[from] = s
	}
	for to, froms := range g.rev {
		s := make(map[string]struct{}, len(froms))
		for from := range froms {
			s[from] = struct{}{}
		}
		c.rev[to] = s
	}
	return c
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
		for to := range c.adj[op.From] {
			delete(c.edges, Edge{From: op.From, To: to})
			delete(c.rev[to], op.From)
		}
		delete(c.adj, op.From)
		for from := range c.rev[op.From] {
			delete(c.edges, Edge{From: from, To: op.From})
			delete(c.adj[from], op.From)
		}
		delete(c.rev, op.From)
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
		addTo(c.adj, op.From, op.To)
		addTo(c.rev, op.To, op.From)
	case DeleteEdge:
		e := Edge{From: op.From, To: op.To}
		if _, ok := c.edges[e]; !ok {
			return ErrNotFound
		}
		delete(c.edges, e)
		delete(c.adj[op.From], op.To)
		delete(c.rev[op.To], op.From)
	default:
		return ErrInvalidInput
	}
	return nil
}

func addTo(m map[string]map[string]struct{}, k, v string) {
	s, ok := m[k]
	if !ok {
		s = make(map[string]struct{})
		m[k] = s
	}
	s[v] = struct{}{}
}

// reachable reports whether dst is reachable from src following edges.
func (c *candidate) reachable(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for to := range c.adj[n] {
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

// commit swaps the candidate state in as the live state.
func (g *Graph) commit(c *candidate) {
	g.nodes = c.nodes
	g.edges = c.edges
	g.adj = c.adj
	g.rev = c.rev
}

// Reachable reports whether dst is reachable from src in a consistent
// snapshot of the current state.
func (g *Graph) Reachable(src, dst string) (bool, error) {
	if err := g.validateName(src); err != nil {
		return false, err
	}
	if err := g.validateName(dst); err != nil {
		return false, err
	}
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
	sort.Strings(s.Nodes)
	for e := range g.edges {
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
