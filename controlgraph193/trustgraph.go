package controlgraph193

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
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
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
		out:   make(map[string]map[string]struct{}),
		in:    make(map[string]map[string]struct{}),
	}, nil
}

func validName(s string, max int) bool {
	if s == "" || len(s) > max {
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

func (g *Graph) validate(op Op) error {
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

func (g *Graph) clone() *Graph {
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
	for k, m := range g.out {
		nm := make(map[string]struct{}, len(m))
		for v := range m {
			nm[v] = struct{}{}
		}
		c.out[k] = nm
	}
	for k, m := range g.in {
		nm := make(map[string]struct{}, len(m))
		for v := range m {
			nm[v] = struct{}{}
		}
		c.in[k] = nm
	}
	return c
}

func (g *Graph) addEdge(from, to string) {
	g.edges[Edge{from, to}] = struct{}{}
	if g.out[from] == nil {
		g.out[from] = make(map[string]struct{})
	}
	g.out[from][to] = struct{}{}
	if g.in[to] == nil {
		g.in[to] = make(map[string]struct{})
	}
	g.in[to][from] = struct{}{}
}

func (g *Graph) removeEdge(from, to string) {
	delete(g.edges, Edge{from, to})
	if m := g.out[from]; m != nil {
		delete(m, to)
		if len(m) == 0 {
			delete(g.out, from)
		}
	}
	if m := g.in[to]; m != nil {
		delete(m, from)
		if len(m) == 0 {
			delete(g.in, to)
		}
	}
}

// reaches reports whether dst is reachable from src following out-edges.
func (g *Graph) reaches(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range g.out[n] {
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

func (g *Graph) Apply(b Batch) (Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Structural validation of the whole batch before touching state.
	for _, op := range b.Ops {
		if err := g.validate(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	c := g.clone()
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
			for to := range c.out[op.From] {
				c.removeEdge(op.From, to)
			}
			for from := range c.in[op.From] {
				c.removeEdge(from, op.From)
			}
			delete(c.nodes, op.From)
		case AddEdge:
			if _, ok := c.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := c.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			e := Edge{op.From, op.To}
			if _, ok := c.edges[e]; ok {
				return Result{}, ErrExists
			}
			if c.reaches(op.To, op.From) {
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
	if len(c.nodes) > c.opts.MaxNodes || len(c.edges) > c.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes = c.nodes
	g.edges = c.edges
	g.out = c.out
	g.in = c.in
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return false, ErrNotFound
	}
	return g.reaches(from, to), nil
}

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
