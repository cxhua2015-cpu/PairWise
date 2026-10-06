package topologygraph283

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

// Graph is a concurrency-safe in-memory directed topology graph.
// The zero value is not usable; construct with New.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[edgeKey]struct{}
	out        map[string]map[string]struct{} // adjacency: from -> set(to)
	generation uint64
}

func New(opts Options) (*Graph, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  opts,
		nodes: make(map[string]struct{}),
		edges: make(map[edgeKey]struct{}),
		out:   make(map[string]map[string]struct{}),
	}, nil
}

// candidate is a mutable working copy used to apply a batch atomically.
type candidate struct {
	nodes map[string]struct{}
	edges map[edgeKey]struct{}
	out   map[string]map[string]struct{}
}

func (g *Graph) newCandidate() *candidate {
	c := &candidate{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[edgeKey]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, tos := range g.out {
		s := make(map[string]struct{}, len(tos))
		for to := range tos {
			s[to] = struct{}{}
		}
		c.out[from] = s
	}
	return c
}

func (c *candidate) addEdge(from, to string) {
	c.edges[edgeKey{from, to}] = struct{}{}
	s := c.out[from]
	if s == nil {
		s = make(map[string]struct{})
		c.out[from] = s
	}
	s[to] = struct{}{}
}

func (c *candidate) deleteEdge(from, to string) {
	delete(c.edges, edgeKey{from, to})
	if s := c.out[from]; s != nil {
		delete(s, to)
		if len(s) == 0 {
			delete(c.out, from)
		}
	}
}

func (c *candidate) deleteNode(name string) {
	delete(c.nodes, name)
	for to := range c.out[name] {
		delete(c.edges, edgeKey{name, to})
	}
	delete(c.out, name)
	for from, tos := range c.out {
		if _, ok := tos[name]; ok {
			delete(c.edges, edgeKey{from, name})
			delete(tos, name)
			if len(tos) == 0 {
				delete(c.out, from)
			}
		}
	}
}

// reachable reports whether dst is reachable from src following out-edges.
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

// applyOp applies a single op to the candidate state.
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
		c.deleteNode(op.From)
	case AddEdge:
		if _, ok := c.nodes[op.From]; !ok {
			return ErrNotFound
		}
		if _, ok := c.nodes[op.To]; !ok {
			return ErrNotFound
		}
		if _, ok := c.edges[edgeKey{op.From, op.To}]; ok {
			return ErrExists
		}
		if c.reachable(op.To, op.From) {
			return ErrCycle
		}
		c.addEdge(op.From, op.To)
	case DeleteEdge:
		if _, ok := c.edges[edgeKey{op.From, op.To}]; !ok {
			return ErrNotFound
		}
		c.deleteEdge(op.From, op.To)
	default:
		return ErrInvalidInput
	}
	return nil
}

// Apply validates and atomically applies a batch. On any error the graph
// is left unchanged. Capacity limits are checked only against the final
// state of the batch.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := validateBatch(b, g.opts); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		res := Result{Generation: g.generation}
		g.mu.RUnlock()
		return res, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	c := g.newCandidate()
	for _, op := range b.Ops {
		if err := c.applyOp(op); err != nil {
			return Result{}, err
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

// Reachable reports whether dst is reachable from src using a consistent
// snapshot of the current state.
func (g *Graph) Reachable(src, dst string) (bool, error) {
	if err := validateName(src, g.opts); err != nil {
		return false, err
	}
	if err := validateName(dst, g.opts); err != nil {
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
	c := &candidate{out: g.out}
	return c.reachable(src, dst), nil
}

// Snapshot returns a consistently ordered, fully detached copy of the
// current state. Nodes are sorted lexicographically; edges are sorted by
// (From, To).
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
		edges = append(edges, Edge{From: e.from, To: e.to})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return Snapshot{Generation: g.generation, Nodes: nodes, Edges: edges}
}
