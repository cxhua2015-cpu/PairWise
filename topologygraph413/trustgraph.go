package topologygraph413

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
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
	generation uint64
}

func New(opts Options) (*Graph, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  opts,
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		out:   make(map[string]map[string]struct{}),
		in:    make(map[string]map[string]struct{}),
	}, nil
}

// candidate is a private transactional copy of the graph state. Ops are
// applied to it; it is committed only if the whole batch succeeds.
type candidate struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

func (g *Graph) candidate() *candidate {
	c := &candidate{
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
	for to := range c.out[name] {
		delete(c.edges, Edge{From: name, To: to})
		delete(c.in[to], name)
	}
	for from := range c.in[name] {
		delete(c.edges, Edge{From: from, To: name})
		delete(c.out[from], name)
	}
	delete(c.out, name)
	delete(c.in, name)
	delete(c.nodes, name)
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
	if c.reachable(to, from) {
		return ErrCycle
	}
	c.edges[e] = struct{}{}
	if c.out[from] == nil {
		c.out[from] = make(map[string]struct{})
	}
	c.out[from][to] = struct{}{}
	if c.in[to] == nil {
		c.in[to] = make(map[string]struct{})
	}
	c.in[to][from] = struct{}{}
	return nil
}

func (c *candidate) deleteEdge(from, to string) error {
	e := Edge{From: from, To: to}
	if _, ok := c.edges[e]; !ok {
		return ErrNotFound
	}
	delete(c.edges, e)
	delete(c.out[from], to)
	delete(c.in[to], from)
	return nil
}

// reachable reports whether dst is reachable from src via out-edges.
func (c *candidate) reachable(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	queue := []string{src}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for next := range c.out[cur] {
			if next == dst {
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

func (c *candidate) apply(op Op) error {
	switch op.Kind {
	case AddNode:
		return c.addNode(op.From)
	case DeleteNode:
		return c.deleteNode(op.From)
	case AddEdge:
		return c.addEdge(op.From, op.To)
	case DeleteEdge:
		return c.deleteEdge(op.From, op.To)
	}
	return ErrInvalidInput
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
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
	c := g.candidate()
	for _, op := range b.Ops {
		if err := c.apply(op); err != nil {
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
	c := &candidate{out: g.out}
	return c.reachable(from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	snap := Snapshot{
		Generation: g.generation,
		Nodes:      make([]string, 0, len(g.nodes)),
		Edges:      make([]Edge, 0, len(g.edges)),
	}
	for n := range g.nodes {
		snap.Nodes = append(snap.Nodes, n)
	}
	for e := range g.edges {
		snap.Edges = append(snap.Edges, e)
	}
	sort.Strings(snap.Nodes)
	sort.Slice(snap.Edges, func(i, j int) bool {
		if snap.Edges[i].From != snap.Edges[j].From {
			return snap.Edges[i].From < snap.Edges[j].From
		}
		return snap.Edges[i].To < snap.Edges[j].To
	})
	return snap
}
