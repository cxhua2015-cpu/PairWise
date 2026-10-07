package topologygraph433

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
//
// Internally it keeps two hash indexes (node set and edge set) guarded by a
// single sync.RWMutex. Transactions execute against a candidate copy of the
// indexes; on success the candidate is swapped in atomically and generation
// advances exactly once, on failure the candidate is discarded and the
// original state is untouched. Every graph owns its maps and slices
// exclusively: clones and snapshots deep-copy, so no ownership is ever shared
// across graphs or with callers.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
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
	}, nil
}

// apply executes the batch against candidate indexes. It assumes the batch
// has already passed structural validation.
func (g *Graph) apply(nodes map[string]struct{}, edges map[Edge]struct{}, b Batch) error {
	for _, op := range b.Ops {
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
			if reachable(nodes, edges, op.To, op.From) {
				return ErrCycle
			}
			edges[e] = struct{}{}
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := edges[e]; !ok {
				return ErrNotFound
			}
			delete(edges, e)
		}
	}
	if len(nodes) > g.opts.MaxNodes || len(edges) > g.opts.MaxEdges {
		return ErrCapacity
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
		return Result{Generation: g.generation}, nil
	}
	nodes, edges := g.copyIndexes()
	if err := g.apply(nodes, edges, b); err != nil {
		return Result{}, err
	}
	g.nodes = nodes
	g.edges = edges
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
	return reachable(g.nodes, g.edges, from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.snapshotLocked()
}

func (g *Graph) snapshotLocked() Snapshot {
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

// copyIndexes deep-copies the node and edge indexes. Callers must hold the lock.
func (g *Graph) copyIndexes() (map[string]struct{}, map[Edge]struct{}) {
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

// reachable reports whether `to` is reachable from `from` following directed
// edges. It runs in O(V+E) using an adjacency list built on demand.
func reachable(nodes map[string]struct{}, edges map[Edge]struct{}, from, to string) bool {
	if from == to {
		return true
	}
	adj := make(map[string][]string, len(nodes))
	for e := range edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, m := range adj[n] {
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
