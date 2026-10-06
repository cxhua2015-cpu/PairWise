package topologygraph293

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
// All public methods may be called concurrently.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{}
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
	}, nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := validateBatch(g.opts, b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		gen := g.generation
		g.mu.RUnlock()
		return Result{Generation: gen}, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	cand := &candidate{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
	}
	for n := range g.nodes {
		cand.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		cand.edges[e] = struct{}{}
	}
	for from, tos := range g.out {
		m := make(map[string]struct{}, len(tos))
		for to := range tos {
			m[to] = struct{}{}
		}
		cand.out[from] = m
	}

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

	g.nodes = cand.nodes
	g.edges = cand.edges
	g.out = cand.out
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
	return reaches(g.out, from, to), nil
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

// candidate is a private transactional copy of the graph state.
type candidate struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
}

func (c *candidate) addNode(name string) error {
	if _, ok := c.nodes[name]; ok {
		return ErrExists
	}
	c.nodes[name] = struct{}{}
	return nil
}

func (c *candidate) deleteNode(name string) error {
	if _, ok := c.nodes[name]; !ok {
		return ErrNotFound
	}
	delete(c.nodes, name)
	for e := range c.edges {
		if e.From == name || e.To == name {
			delete(c.edges, e)
		}
	}
	delete(c.out, name)
	for _, tos := range c.out {
		delete(tos, name)
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
	if reaches(c.out, to, from) {
		return ErrCycle
	}
	c.edges[e] = struct{}{}
	if c.out[from] == nil {
		c.out[from] = make(map[string]struct{})
	}
	c.out[from][to] = struct{}{}
	return nil
}

func (c *candidate) deleteEdge(from, to string) error {
	e := Edge{From: from, To: to}
	if _, ok := c.edges[e]; !ok {
		return ErrNotFound
	}
	delete(c.edges, e)
	delete(c.out[from], to)
	return nil
}

// reaches reports whether target is reachable from start via out edges.
func reaches(out map[string]map[string]struct{}, start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	stack := []string{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range out[n] {
			if next == target {
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
