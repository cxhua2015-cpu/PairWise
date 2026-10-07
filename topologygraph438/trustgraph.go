package topologygraph438

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
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	adj        map[string]map[string]struct{}
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
		adj:   make(map[string]map[string]struct{}),
	}, nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		defer g.mu.RUnlock()
		return Result{Generation: g.generation}, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	edges := make(map[Edge]struct{}, len(g.edges))
	adj := make(map[string]map[string]struct{}, len(g.adj))
	for e := range g.edges {
		edges[e] = struct{}{}
		if adj[e.From] == nil {
			adj[e.From] = make(map[string]struct{})
		}
		adj[e.From][e.To] = struct{}{}
	}

	for _, op := range b.Ops {
		if err := applyOp(op, nodes, edges, adj); err != nil {
			return Result{}, err
		}
	}
	if len(nodes) > g.opts.MaxNodes || len(edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges, g.adj = nodes, edges, adj
	g.generation++
	return Result{Generation: g.generation}, nil
}

func applyOp(op Op, nodes map[string]struct{}, edges map[Edge]struct{}, adj map[string]map[string]struct{}) error {
	switch op.Kind {
	case AddNode:
		if _, ok := nodes[op.From]; ok {
			return ErrExists
		}
		nodes[op.From] = struct{}{}
	case DeleteNode:
		if _, ok := nodes[op.From]; !ok {
			return ErrNotFound
		}
		delete(nodes, op.From)
		for e := range edges {
			if e.From == op.From || e.To == op.From {
				delete(edges, e)
				delete(adj[e.From], e.To)
			}
		}
	case AddEdge:
		if _, ok := nodes[op.From]; !ok {
			return ErrNotFound
		}
		if _, ok := nodes[op.To]; !ok {
			return ErrNotFound
		}
		e := Edge{From: op.From, To: op.To}
		if _, ok := edges[e]; ok {
			return ErrExists
		}
		if reaches(adj, op.To, op.From) {
			return ErrCycle
		}
		edges[e] = struct{}{}
		if adj[op.From] == nil {
			adj[op.From] = make(map[string]struct{})
		}
		adj[op.From][op.To] = struct{}{}
	case DeleteEdge:
		e := Edge{From: op.From, To: op.To}
		if _, ok := edges[e]; !ok {
			return ErrNotFound
		}
		delete(edges, e)
		delete(adj[op.From], op.To)
	}
	return nil
}

func reaches(adj map[string]map[string]struct{}, from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range adj[n] {
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

func (g *Graph) Reachable(from, to string) (bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return false, ErrNotFound
	}
	return reaches(g.adj, from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.snapshotLocked()
}

func (g *Graph) snapshotLocked() Snapshot {
	s := Snapshot{Generation: g.generation, Nodes: make([]string, 0, len(g.nodes)), Edges: make([]Edge, 0, len(g.edges))}
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
