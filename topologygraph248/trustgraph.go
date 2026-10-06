package topologygraph248

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

// Graph is a concurrency-safe in-memory control topology graph.
type Graph struct {
	mu    sync.RWMutex
	opts  Options
	gen   uint64
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
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

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}
	c := g.candidate()
	for _, op := range b.Ops {
		if err := c.apply(op); err != nil {
			return Result{}, err
		}
	}
	if len(c.nodes) > g.opts.MaxNodes || len(c.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	c.commit(g)
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
	return reachable(g.out, from, to), nil
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

// reachable reports whether to is reachable from from via out edges (BFS).
func reachable(out map[string]map[string]struct{}, from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	queue := []string{from}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for m := range out[n] {
			if m == to {
				return true
			}
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				queue = append(queue, m)
			}
		}
	}
	return false
}

// state is a candidate transaction view of the graph indexes.
type state struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

// candidate builds a private copy of the committed indexes for a transaction.
func (g *Graph) candidate() *state {
	c := &state{
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
	for n, set := range g.out {
		ns := make(map[string]struct{}, len(set))
		for m := range set {
			ns[m] = struct{}{}
		}
		c.out[n] = ns
	}
	for n, set := range g.in {
		ns := make(map[string]struct{}, len(set))
		for m := range set {
			ns[m] = struct{}{}
		}
		c.in[n] = ns
	}
	return c
}

// commit transfers candidate ownership to g; the candidate must not be reused.
func (c *state) commit(g *Graph) {
	g.nodes, g.edges, g.out, g.in = c.nodes, c.edges, c.out, c.in
}

func (c *state) apply(op Op) error {
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
		for m := range c.out[op.From] {
			delete(c.edges, Edge{op.From, m})
			delete(c.in[m], op.From)
			if len(c.in[m]) == 0 {
				delete(c.in, m)
			}
		}
		delete(c.out, op.From)
		for m := range c.in[op.From] {
			delete(c.edges, Edge{m, op.From})
			delete(c.out[m], op.From)
			if len(c.out[m]) == 0 {
				delete(c.out, m)
			}
		}
		delete(c.in, op.From)
	case AddEdge:
		e := Edge{op.From, op.To}
		if _, ok := c.nodes[op.From]; !ok {
			return ErrNotFound
		}
		if _, ok := c.nodes[op.To]; !ok {
			return ErrNotFound
		}
		if _, ok := c.edges[e]; ok {
			return ErrExists
		}
		if reachable(c.out, op.To, op.From) {
			return ErrCycle
		}
		c.edges[e] = struct{}{}
		if c.out[op.From] == nil {
			c.out[op.From] = make(map[string]struct{})
		}
		c.out[op.From][op.To] = struct{}{}
		if c.in[op.To] == nil {
			c.in[op.To] = make(map[string]struct{})
		}
		c.in[op.To][op.From] = struct{}{}
	case DeleteEdge:
		e := Edge{op.From, op.To}
		if _, ok := c.edges[e]; !ok {
			return ErrNotFound
		}
		delete(c.edges, e)
		delete(c.out[op.From], op.To)
		if len(c.out[op.From]) == 0 {
			delete(c.out, op.From)
		}
		delete(c.in[op.To], op.From)
		if len(c.in[op.To]) == 0 {
			delete(c.in, op.To)
		}
	}
	return nil
}
