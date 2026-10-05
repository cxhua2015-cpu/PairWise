package controlgraph158

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

type Graph struct {
	mu    sync.RWMutex
	opts  Options
	gen   uint64
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

func validName(s string, max int) bool {
	if s == "" || len(s) > max {
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

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  o,
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		out:   make(map[string]map[string]struct{}),
		in:    make(map[string]map[string]struct{}),
	}, nil
}

// validate checks the whole batch structurally before any state is read.
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

// candidate is a transactional copy mutated by a batch and committed on success.
type candidate struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

func cloneSet(m map[string]struct{}) map[string]struct{} {
	c := make(map[string]struct{}, len(m))
	for k := range m {
		c[k] = struct{}{}
	}
	return c
}

func cloneAdj(m map[string]map[string]struct{}) map[string]map[string]struct{} {
	c := make(map[string]map[string]struct{}, len(m))
	for k, v := range m {
		c[k] = cloneSet(v)
	}
	return c
}

func (g *Graph) newCandidate() *candidate {
	edges := make(map[Edge]struct{}, len(g.edges))
	for e := range g.edges {
		edges[e] = struct{}{}
	}
	return &candidate{
		nodes: cloneSet(g.nodes),
		edges: edges,
		out:   cloneAdj(g.out),
		in:    cloneAdj(g.in),
	}
}

func (c *candidate) addEdge(from, to string) {
	c.edges[Edge{from, to}] = struct{}{}
	if c.out[from] == nil {
		c.out[from] = make(map[string]struct{})
	}
	c.out[from][to] = struct{}{}
	if c.in[to] == nil {
		c.in[to] = make(map[string]struct{})
	}
	c.in[to][from] = struct{}{}
}

func (c *candidate) removeEdge(from, to string) {
	delete(c.edges, Edge{from, to})
	delete(c.out[from], to)
	delete(c.in[to], from)
}

func (c *candidate) removeNode(n string) {
	delete(c.nodes, n)
	for to := range c.out[n] {
		c.removeEdge(n, to)
	}
	for from := range c.in[n] {
		c.removeEdge(from, n)
	}
	delete(c.out, n)
	delete(c.in, n)
}

// createsCycle reports whether adding from->to would close a directed cycle.
func (c *candidate) createsCycle(from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{to: {}}
	stack := []string{to}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == from {
			return true
		}
		for next := range c.out[n] {
			if _, ok := seen[next]; !ok {
				seen[next] = struct{}{}
				stack = append(stack, next)
			}
		}
	}
	return false
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
	c := g.newCandidate()
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
			c.removeNode(op.From)
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
			if c.createsCycle(op.From, op.To) {
				return Result{}, ErrCycle
			}
			c.addEdge(op.From, op.To)
		case DeleteEdge:
			if _, ok := c.edges[Edge{op.From, op.To}]; !ok {
				return Result{}, ErrNotFound
			}
			c.removeEdge(op.From, op.To)
		}
	}
	if len(c.nodes) > g.opts.MaxNodes || len(c.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges, g.out, g.in = c.nodes, c.edges, c.out, c.in
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
	if from == to {
		return true, nil
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range g.out[n] {
			if next == to {
				return true, nil
			}
			if _, ok := seen[next]; !ok {
				seen[next] = struct{}{}
				stack = append(stack, next)
			}
		}
	}
	return false, nil
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
	return Snapshot{Generation: g.gen, Nodes: nodes, Edges: edges}
}
