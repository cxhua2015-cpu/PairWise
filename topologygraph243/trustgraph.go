package topologygraph243

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
	if err := g.validateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	edges := make(map[Edge]struct{}, len(g.edges))
	for e := range g.edges {
		edges[e] = struct{}{}
	}
	adj := make(map[string]map[string]struct{}, len(g.adj))
	for from, tos := range g.adj {
		s := make(map[string]struct{}, len(tos))
		for to := range tos {
			s[to] = struct{}{}
		}
		adj[from] = s
	}

	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = applyAddNode(nodes, op.From)
		case DeleteNode:
			err = applyDeleteNode(nodes, edges, adj, op.From)
		case AddEdge:
			err = applyAddEdge(nodes, edges, adj, op.From, op.To)
		case DeleteEdge:
			err = applyDeleteEdge(edges, adj, op.From, op.To)
		}
		if err != nil {
			return Result{}, err
		}
	}
	if len(nodes) > g.opts.MaxNodes || len(edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes = nodes
	g.edges = edges
	g.adj = adj
	if len(b.Ops) > 0 {
		g.generation++
	}
	return Result{Generation: g.generation}, nil
}

func applyAddNode(nodes map[string]struct{}, name string) error {
	if _, ok := nodes[name]; ok {
		return ErrExists
	}
	nodes[name] = struct{}{}
	return nil
}

func applyDeleteNode(nodes map[string]struct{}, edges map[Edge]struct{}, adj map[string]map[string]struct{}, name string) error {
	if _, ok := nodes[name]; !ok {
		return ErrNotFound
	}
	delete(nodes, name)
	for to := range adj[name] {
		delete(edges, Edge{From: name, To: to})
	}
	delete(adj, name)
	for from, tos := range adj {
		if _, ok := tos[name]; ok {
			delete(tos, name)
			delete(edges, Edge{From: from, To: name})
		}
	}
	return nil
}

func applyAddEdge(nodes map[string]struct{}, edges map[Edge]struct{}, adj map[string]map[string]struct{}, from, to string) error {
	if _, ok := nodes[from]; !ok {
		return ErrNotFound
	}
	if _, ok := nodes[to]; !ok {
		return ErrNotFound
	}
	e := Edge{From: from, To: to}
	if _, ok := edges[e]; ok {
		return ErrExists
	}
	if reachableFrom(adj, to, from) {
		return ErrCycle
	}
	edges[e] = struct{}{}
	if adj[from] == nil {
		adj[from] = make(map[string]struct{})
	}
	adj[from][to] = struct{}{}
	return nil
}

func applyDeleteEdge(edges map[Edge]struct{}, adj map[string]map[string]struct{}, from, to string) error {
	e := Edge{From: from, To: to}
	if _, ok := edges[e]; !ok {
		return ErrNotFound
	}
	delete(edges, e)
	delete(adj[from], to)
	if len(adj[from]) == 0 {
		delete(adj, from)
	}
	return nil
}

func reachableFrom(adj map[string]map[string]struct{}, start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	stack := []string{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range adj[n] {
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

func (g *Graph) Reachable(from, to string) (bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return false, ErrNotFound
	}
	return reachableFrom(g.adj, from, to), nil
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
	return Snapshot{Generation: g.generation, Nodes: nodes, Edges: edges}
}
