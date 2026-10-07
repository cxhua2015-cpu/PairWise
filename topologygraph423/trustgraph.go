package topologygraph423

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
	}, nil
}

// Apply validates the batch structurally, then applies it atomically against
// the current state. On any failure the graph is left unchanged.
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

	g.nodes = nodes
	g.edges = edges
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

// reachable reports whether dst is reachable from src in the given edge set.
func reachable(edges map[Edge]struct{}, src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for e := range edges {
			if e.From != cur {
				continue
			}
			if e.To == dst {
				return true
			}
			if _, ok := seen[e.To]; !ok {
				seen[e.To] = struct{}{}
				stack = append(stack, e.To)
			}
		}
	}
	return false
}

// Reachable reports whether dst is reachable from src using a consistent
// snapshot of the current state.
func (g *Graph) Reachable(src, dst string) (bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[src]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[dst]; !ok {
		return false, ErrNotFound
	}
	return reachable(g.edges, src, dst), nil
}

// Snapshot returns a consistently ordered, fully independent copy of the
// current state.
func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.snapshotLocked()
}

func (g *Graph) snapshotLocked() Snapshot {
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
