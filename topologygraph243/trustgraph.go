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

// Graph is a concurrency-safe in-memory control topology graph.
// All state transitions happen through atomic batches guarded by mu.
type Graph struct {
	mu    sync.RWMutex
	opts  Options
	nodes map[string]struct{}
	edges map[Edge]struct{}
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
	}, nil
}

// Apply validates the batch structurally, replays it against a private
// candidate state, checks final capacity, and commits atomically.
// Any failure rolls the whole batch back.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}
	nodes, edges := g.candidate()
	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = applyAddNode(nodes, op.From)
		case DeleteNode:
			err = applyDeleteNode(nodes, edges, op.From)
		case AddEdge:
			err = applyAddEdge(nodes, edges, op.From, op.To)
		case DeleteEdge:
			err = applyDeleteEdge(edges, op.From, op.To)
		}
		if err != nil {
			return Result{}, err
		}
	}
	if len(nodes) > g.opts.MaxNodes || len(edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges = nodes, edges
	g.gen++
	return Result{Generation: g.gen}, nil
}

// candidate builds the private working copy a batch mutates before commit.
func (g *Graph) candidate() (map[string]struct{}, map[Edge]struct{}) {
	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	edges := make(map[Edge]struct{}, len(g.edges))
	for e := range g.edges {
		edges[e] = struct{}{}
	}
	return nodes, edges
}

func applyAddNode(nodes map[string]struct{}, name string) error {
	if _, ok := nodes[name]; ok {
		return ErrExists
	}
	nodes[name] = struct{}{}
	return nil
}

func applyDeleteNode(nodes map[string]struct{}, edges map[Edge]struct{}, name string) error {
	if _, ok := nodes[name]; !ok {
		return ErrNotFound
	}
	delete(nodes, name)
	for e := range edges {
		if e.From == name || e.To == name {
			delete(edges, e)
		}
	}
	return nil
}

func applyAddEdge(nodes map[string]struct{}, edges map[Edge]struct{}, from, to string) error {
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
	if reachable(edges, to, from) {
		return ErrCycle
	}
	edges[e] = struct{}{}
	return nil
}

func applyDeleteEdge(edges map[Edge]struct{}, from, to string) error {
	e := Edge{From: from, To: to}
	if _, ok := edges[e]; !ok {
		return ErrNotFound
	}
	delete(edges, e)
	return nil
}

// reachable reports whether dst is reachable from src following edges.
func reachable(edges map[Edge]struct{}, src, dst string) bool {
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == dst {
			return true
		}
		for e := range edges {
			if e.From == n {
				if _, ok := seen[e.To]; !ok {
					seen[e.To] = struct{}{}
					stack = append(stack, e.To)
				}
			}
		}
	}
	return false
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
	return reachable(g.edges, from, to), nil
}

// Snapshot returns a consistent view with nodes and edges stably sorted.
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
