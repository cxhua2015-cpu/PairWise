package controlgraph153

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
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
	edges int
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
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validateOp performs structural validation only; it does not read state.
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

// candidate is a mutable copy of the graph state used to apply a batch
// transactionally. It is discarded on failure.
type candidate struct {
	nodes map[string]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
	edges int
}

func (g *Graph) fork() *candidate {
	c := &candidate{
		nodes: make(map[string]struct{}, len(g.nodes)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
		in:    make(map[string]map[string]struct{}, len(g.in)),
		edges: g.edges,
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
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
		for next := range c.out[n] {
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

func (c *candidate) addEdge(from, to string) {
	if c.out[from] == nil {
		c.out[from] = make(map[string]struct{})
	}
	c.out[from][to] = struct{}{}
	if c.in[to] == nil {
		c.in[to] = make(map[string]struct{})
	}
	c.in[to][from] = struct{}{}
	c.edges++
}

func (c *candidate) removeEdge(from, to string) {
	delete(c.out[from], to)
	if len(c.out[from]) == 0 {
		delete(c.out, from)
	}
	delete(c.in[to], from)
	if len(c.in[to]) == 0 {
		delete(c.in, to)
	}
	c.edges--
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
		for to := range c.out[op.From] {
			c.removeEdge(op.From, to)
		}
		for from := range c.in[op.From] {
			c.removeEdge(from, op.From)
		}
	case AddEdge:
		if _, ok := c.nodes[op.From]; !ok {
			return ErrNotFound
		}
		if _, ok := c.nodes[op.To]; !ok {
			return ErrNotFound
		}
		if _, ok := c.out[op.From][op.To]; ok {
			return ErrExists
		}
		if c.reachable(op.To, op.From) {
			return ErrCycle
		}
		c.addEdge(op.From, op.To)
	case DeleteEdge:
		if _, ok := c.out[op.From][op.To]; !ok {
			return ErrNotFound
		}
		c.removeEdge(op.From, op.To)
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	// Structural validation of the whole batch before touching state.
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}
	c := g.fork()
	for _, op := range b.Ops {
		if err := c.apply(op); err != nil {
			return Result{}, err
		}
	}
	// Capacity is checked only on the final state of the batch.
	if len(c.nodes) > g.opts.MaxNodes || c.edges > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.out, g.in, g.edges = c.nodes, c.out, c.in, c.edges
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
		Edges:      make([]Edge, 0, g.edges),
	}
	for n := range g.nodes {
		s.Nodes = append(s.Nodes, n)
	}
	sort.Strings(s.Nodes)
	for from, set := range g.out {
		for to := range set {
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
