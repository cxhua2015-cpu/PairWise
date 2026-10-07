package topologygraph303

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
	nodes map[string]struct{}
	edges map[Edge]struct{}
	adj   map[string]map[string]struct{}
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
		adj:   make(map[string]map[string]struct{}),
	}, nil
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

// validate checks the whole batch structurally before any state is read.
func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if !validName(op.From, g.opts.MaxNameBytes) || op.To != "" {
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

// candidate is a scratch copy of the graph mutated by a batch; it is
// committed only if the whole batch succeeds.
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
		_, ok := c.nodes[from]
		return ok
	}
	seen := map[string]struct{}{from: {}}
	queue := []string{from}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for next := range c.adj[n] {
			if next == to {
				return true
			}
			if _, ok := seen[next]; !ok {
				seen[next] = struct{}{}
				queue = append(queue, next)
			}
		}
	}
	return false
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
	if from == to || c.reachable(to, from) {
		return ErrCycle
	}
	c.edges[e] = struct{}{}
	if c.adj[from] == nil {
		c.adj[from] = make(map[string]struct{})
	}
	c.adj[from][to] = struct{}{}
	return nil
}

func (c *candidate) deleteNode(n string) error {
	if _, ok := c.nodes[n]; !ok {
		return ErrNotFound
	}
	delete(c.nodes, n)
	for to := range c.adj[n] {
		delete(c.edges, Edge{From: n, To: to})
	}
	delete(c.adj, n)
	for from, tos := range c.adj {
		if _, ok := tos[n]; ok {
			delete(tos, n)
			delete(c.edges, Edge{From: from, To: n})
		}
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.validate(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}
	c := g.fork()
	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			if _, ok := c.nodes[op.From]; ok {
				err = ErrExists
			} else {
				c.nodes[op.From] = struct{}{}
			}
		case DeleteNode:
			err = c.deleteNode(op.From)
		case AddEdge:
			err = c.addEdge(op.From, op.To)
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := c.edges[e]; !ok {
				err = ErrNotFound
			} else {
				delete(c.edges, e)
				delete(c.adj[op.From], op.To)
			}
		}
		if err != nil {
			return Result{}, err
		}
	}
	if len(c.nodes) > g.opts.MaxNodes || len(c.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes = c.nodes
	g.edges = c.edges
	g.adj = c.adj
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
	c := &candidate{nodes: g.nodes, adj: g.adj}
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
