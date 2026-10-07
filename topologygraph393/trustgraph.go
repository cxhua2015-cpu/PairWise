// Package topologygraph393 implements a concurrency-safe in-memory
// directed control-topology graph with atomic batch mutations.
package topologygraph393

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

// Graph is a concurrency-safe in-memory directed acyclic topology graph.
//
// State is kept in three indexes:
//   - nodes: set of node names
//   - edges: set of directed edges for O(1) existence checks
//   - out:   adjacency index from -> set(to) for traversal and cascade deletes
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

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validate performs full structural validation of the batch before any
// state is read: unknown kinds, extra fields, and malformed names are
// rejected with ErrInvalidInput.
func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
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
	}
	return nil
}

// candidate is a private copy of the graph state that a batch mutates.
// It is committed only if the whole batch succeeds, giving atomic rollback
// for free: a failed candidate is simply discarded.
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
	for f, tos := range g.out {
		s := make(map[string]struct{}, len(tos))
		for t := range tos {
			s[t] = struct{}{}
		}
		c.out[f] = s
	}
	return c
}

func (c *candidate) addEdge(e Edge) {
	c.edges[e] = struct{}{}
	s := c.out[e.From]
	if s == nil {
		s = make(map[string]struct{})
		c.out[e.From] = s
	}
	s[e.To] = struct{}{}
}

func (c *candidate) delEdge(e Edge) {
	delete(c.edges, e)
	if s := c.out[e.From]; s != nil {
		delete(s, e.To)
		if len(s) == 0 {
			delete(c.out, e.From)
		}
	}
}

// reaches reports whether dst is reachable from src following out-edges.
func (c *candidate) reaches(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for t := range c.out[n] {
			if t == dst {
				return true
			}
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				stack = append(stack, t)
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
		// Cascade: remove every edge touching the node.
		for t := range c.out[op.From] {
			delete(c.edges, Edge{From: op.From, To: t})
		}
		delete(c.out, op.From)
		for f, tos := range c.out {
			if _, ok := tos[op.From]; ok {
				delete(tos, op.From)
				delete(c.edges, Edge{From: f, To: op.From})
				if len(tos) == 0 {
					delete(c.out, f)
				}
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
		// Adding from->to closes a cycle iff `to` already reaches `from`.
		if c.reaches(op.To, op.From) {
			return ErrCycle
		}
		c.addEdge(e)
	case DeleteEdge:
		e := Edge{From: op.From, To: op.To}
		if _, ok := c.edges[e]; !ok {
			return ErrNotFound
		}
		c.delEdge(e)
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.validate(b); err != nil {
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
	c := g.fork()
	for _, op := range b.Ops {
		if err := c.apply(op); err != nil {
			return Result{}, err
		}
	}
	// Capacity is enforced on the final state of the batch only.
	if len(c.nodes) > g.opts.MaxNodes || len(c.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges, g.out = c.nodes, c.edges, c.out
	g.gen++
	return Result{Generation: g.gen}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.opts.MaxNameBytes) || !validName(to, g.opts.MaxNameBytes) {
		return false, ErrInvalidInput
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return false, ErrNotFound
	}
	c := &candidate{out: g.out}
	return c.reaches(from, to), nil
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
