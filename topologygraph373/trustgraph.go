package topologygraph373

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
	maxNodes   int
	maxEdges   int
	maxName    int
	generation uint64
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	adj        map[string]map[string]struct{}
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxNodes: o.MaxNodes,
		maxEdges: o.MaxEdges,
		maxName:  o.MaxNameBytes,
		nodes:    make(map[string]struct{}),
		edges:    make(map[Edge]struct{}),
		adj:      make(map[string]map[string]struct{}),
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

// validateOp performs structural validation only; it must not read graph state.
func (g *Graph) validateOp(op Op) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" || !validName(op.From, g.maxName) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(op.From, g.maxName) || !validName(op.To, g.maxName) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// candidate is a transactional copy of the graph state mutated by a batch.
type candidate struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	adj   map[string]map[string]struct{}
}

func (g *Graph) fork() *candidate {
	c := &candidate{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		adj:   make(map[string]map[string]struct{}, len(g.adj)),
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, tos := range g.adj {
		m := make(map[string]struct{}, len(tos))
		for to := range tos {
			m[to] = struct{}{}
		}
		c.adj[from] = m
	}
	return c
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
		for next := range c.adj[n] {
			if next == to {
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
	for to := range c.adj[n] {
		delete(c.edges, Edge{n, to})
	}
	delete(c.adj, n)
	for from, tos := range c.adj {
		if _, ok := tos[n]; ok {
			delete(tos, n)
			delete(c.edges, Edge{from, n})
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
	e := Edge{from, to}
	if _, ok := c.edges[e]; ok {
		return ErrExists
	}
	if c.reachable(to, from) {
		return ErrCycle
	}
	c.edges[e] = struct{}{}
	if c.adj[from] == nil {
		c.adj[from] = make(map[string]struct{})
	}
	c.adj[from][to] = struct{}{}
	return nil
}

func (c *candidate) deleteEdge(from, to string) error {
	e := Edge{from, to}
	if _, ok := c.edges[e]; !ok {
		return ErrNotFound
	}
	delete(c.edges, e)
	delete(c.adj[from], to)
	if len(c.adj[from]) == 0 {
		delete(c.adj, from)
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	c := g.fork()
	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = c.addNode(op.From)
		case DeleteNode:
			err = c.deleteNode(op.From)
		case AddEdge:
			err = c.addEdge(op.From, op.To)
		case DeleteEdge:
			err = c.deleteEdge(op.From, op.To)
		}
		if err != nil {
			return Result{}, err
		}
	}
	if len(c.nodes) > g.maxNodes || len(c.edges) > g.maxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes = c.nodes
	g.edges = c.edges
	g.adj = c.adj
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.maxName) || !validName(to, g.maxName) {
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
	c := &candidate{adj: g.adj}
	return c.reachable(from, to), nil
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
