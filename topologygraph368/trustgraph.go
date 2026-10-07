package topologygraph368

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

type Graph struct {
	mu    sync.RWMutex
	opts  Options
	nodes map[string]struct{}
	edges map[edgeKey]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
	gen   uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  o,
		nodes: make(map[string]struct{}),
		edges: make(map[edgeKey]struct{}),
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

func validateOp(o Op, maxName int) error {
	switch o.Kind {
	case AddNode, DeleteNode:
		if o.To != "" || !validName(o.From, maxName) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(o.From, maxName) || !validName(o.To, maxName) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// candidate is a mutable transactional view over the graph state.
type candidate struct {
	nodes map[string]struct{}
	edges map[edgeKey]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

func (g *Graph) newCandidate() *candidate {
	c := &candidate{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[edgeKey]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
		in:    make(map[string]map[string]struct{}, len(g.in)),
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for k, v := range g.out {
		s := make(map[string]struct{}, len(v))
		for x := range v {
			s[x] = struct{}{}
		}
		c.out[k] = s
	}
	for k, v := range g.in {
		s := make(map[string]struct{}, len(v))
		for x := range v {
			s[x] = struct{}{}
		}
		c.in[k] = s
	}
	return c
}

func (c *candidate) addEdge(from, to string) {
	c.edges[edgeKey{from, to}] = struct{}{}
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
	delete(c.edges, edgeKey{from, to})
	if s := c.out[from]; s != nil {
		delete(s, to)
		if len(s) == 0 {
			delete(c.out, from)
		}
	}
	if s := c.in[to]; s != nil {
		delete(s, from)
		if len(s) == 0 {
			delete(c.in, to)
		}
	}
}

func (c *candidate) reachable(from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range c.out[n] {
			if m == to {
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

func (c *candidate) apply(o Op) error {
	switch o.Kind {
	case AddNode:
		if _, ok := c.nodes[o.From]; ok {
			return ErrExists
		}
		c.nodes[o.From] = struct{}{}
	case DeleteNode:
		if _, ok := c.nodes[o.From]; !ok {
			return ErrNotFound
		}
		for to := range c.out[o.From] {
			c.removeEdge(o.From, to)
		}
		for from := range c.in[o.From] {
			c.removeEdge(from, o.From)
		}
		delete(c.nodes, o.From)
	case AddEdge:
		if _, ok := c.nodes[o.From]; !ok {
			return ErrNotFound
		}
		if _, ok := c.nodes[o.To]; !ok {
			return ErrNotFound
		}
		if _, ok := c.edges[edgeKey{o.From, o.To}]; ok {
			return ErrExists
		}
		if c.reachable(o.To, o.From) {
			return ErrCycle
		}
		c.addEdge(o.From, o.To)
	case DeleteEdge:
		if _, ok := c.edges[edgeKey{o.From, o.To}]; !ok {
			return ErrNotFound
		}
		c.removeEdge(o.From, o.To)
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Full structural validation before touching any state.
	for _, o := range b.Ops {
		if err := validateOp(o, g.opts.MaxNameBytes); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}

	c := g.newCandidate()
	for _, o := range b.Ops {
		if err := c.apply(o); err != nil {
			return Result{}, err
		}
	}
	if len(c.nodes) > g.opts.MaxNodes || len(c.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes = c.nodes
	g.edges = c.edges
	g.out = c.out
	g.in = c.in
	g.gen++
	return Result{Generation: g.gen}, nil
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
	c := &candidate{out: g.out}
	return c.reachable(from, to), nil
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
	sort.Strings(s.Nodes)
	for e := range g.edges {
		s.Edges = append(s.Edges, Edge{From: e.from, To: e.to})
	}
	sort.Slice(s.Edges, func(i, j int) bool {
		if s.Edges[i].From != s.Edges[j].From {
			return s.Edges[i].From < s.Edges[j].From
		}
		return s.Edges[i].To < s.Edges[j].To
	})
	return s
}
