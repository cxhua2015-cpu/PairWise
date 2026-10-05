// Package rolloutgraph implements a concurrency-safe, in-memory
// directed release-dependency graph with atomic batch mutations.
package rolloutgraph

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

// Graph is a concurrency-safe in-memory dependency graph.
//
// Internally it keeps three indexes guarded by a single RWMutex:
// nodes (set of names), out (from -> set of to) and in (to -> set of
// from). The in index exists so DeleteNode can cascade-delete incident
// edges without scanning the whole edge set.
type Graph struct {
	mu     sync.RWMutex
	opts   Options
	gen    uint64
	nodes  map[string]struct{}
	out    map[string]map[string]struct{}
	in     map[string]map[string]struct{}
	nEdges int
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  o,
		nodes: make(map[string]struct{}),
		out:   make(map[string]map[string]struct{}),
		in:    make(map[string]map[string]struct{}),
	}, nil
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validateOp performs purely structural validation: it never reads graph
// state, so the whole batch is checked before any state is touched.
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

// candidate is a speculative copy of the graph indexes. A batch is
// executed against a candidate; only on success is it swapped in, which
// makes rollback trivial and atomic.
type candidate struct {
	nodes  map[string]struct{}
	out    map[string]map[string]struct{}
	in     map[string]map[string]struct{}
	nEdges int
}

func (g *Graph) fork() *candidate {
	c := &candidate{
		nodes:  make(map[string]struct{}, len(g.nodes)),
		out:    make(map[string]map[string]struct{}, len(g.out)),
		in:     make(map[string]map[string]struct{}, len(g.in)),
		nEdges: g.nEdges,
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for from, tos := range g.out {
		s := make(map[string]struct{}, len(tos))
		for to := range tos {
			s[to] = struct{}{}
		}
		c.out[from] = s
	}
	for to, froms := range g.in {
		s := make(map[string]struct{}, len(froms))
		for from := range froms {
			s[from] = struct{}{}
		}
		c.in[to] = s
	}
	return c
}

func (c *candidate) hasEdge(from, to string) bool {
	_, ok := c.out[from][to]
	return ok
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
		for m := range c.out[n] {
			if m == dst {
				return true
			}
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				stack = append(stack, m)
			}
		}
	}
	return false
}

func (c *candidate) addEdge(from, to string) {
	s := c.out[from]
	if s == nil {
		s = make(map[string]struct{})
		c.out[from] = s
	}
	s[to] = struct{}{}
	s = c.in[to]
	if s == nil {
		s = make(map[string]struct{})
		c.in[to] = s
	}
	s[from] = struct{}{}
	c.nEdges++
}

func (c *candidate) delEdge(from, to string) {
	delete(c.out[from], to)
	if len(c.out[from]) == 0 {
		delete(c.out, from)
	}
	delete(c.in[to], from)
	if len(c.in[to]) == 0 {
		delete(c.in, to)
	}
	c.nEdges--
}

func (c *candidate) delNode(n string) {
	for to := range c.out[n] {
		c.delEdge(n, to)
	}
	for from := range c.in[n] {
		c.delEdge(from, n)
	}
	delete(c.nodes, n)
}

func (g *Graph) Apply(b Batch) (Result, error) {
	// Phase 1: structural validation of the whole batch before any
	// state is read or mutated.
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
			c.delNode(op.From)
		case AddEdge:
			if _, ok := c.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := c.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if c.hasEdge(op.From, op.To) {
				return Result{}, ErrExists
			}
			if c.reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			c.addEdge(op.From, op.To)
		case DeleteEdge:
			if !c.hasEdge(op.From, op.To) {
				return Result{}, ErrNotFound
			}
			c.delEdge(op.From, op.To)
		}
	}
	// Capacity is enforced only on the final state of the batch.
	if len(c.nodes) > g.opts.MaxNodes || c.nEdges > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes, g.out, g.in, g.nEdges = c.nodes, c.out, c.in, c.nEdges
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
	return c.reachable(from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := Snapshot{
		Generation: g.gen,
		Nodes:      make([]string, 0, len(g.nodes)),
		Edges:      make([]Edge, 0, g.nEdges),
	}
	for n := range g.nodes {
		s.Nodes = append(s.Nodes, n)
	}
	sort.Strings(s.Nodes)
	for from, tos := range g.out {
		for to := range tos {
			s.Edges = append(s.Edges, Edge{From: from, To: to})
		}
	}
	sort.Slice(s.Edges, func(i, j int) bool {
		if s.Edges[i].From != s.Edges[j].From {
			return s.Edges[i].From < s.Edges[j].From
		}
		return s.Edges[i].To < s.Edges[j].To
	})
	return s
}
