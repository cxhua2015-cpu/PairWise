package topologygraph263

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
//
// Index structure: nodes and edges are kept in hash maps for O(1)
// membership checks; adjacency (out-edges per node) is maintained for
// fast reachability and cascade deletion. All public methods are safe
// for concurrent use; readers take RLock, Apply takes the write lock.
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

// Apply validates the batch structurally, then executes it against a
// candidate copy of the state. Capacity limits are checked only at the
// end of the batch; any failure discards the candidate (full rollback).
// A non-empty successful batch increments generation exactly once.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		r := Result{Generation: g.generation}
		g.mu.RUnlock()
		return r, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	// Candidate transaction: work on private copies so a failed batch
	// leaves the committed state untouched.
	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	edges := make(map[Edge]struct{}, len(g.edges))
	for e := range g.edges {
		edges[e] = struct{}{}
	}
	adj := make(map[string]map[string]struct{}, len(g.adj))
	for from, set := range g.adj {
		s := make(map[string]struct{}, len(set))
		for to := range set {
			s[to] = struct{}{}
		}
		adj[from] = s
	}

	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			if _, ok := nodes[op.From]; ok {
				err = ErrExists
			} else {
				nodes[op.From] = struct{}{}
			}
		case DeleteNode:
			if _, ok := nodes[op.From]; !ok {
				err = ErrNotFound
			} else {
				delete(nodes, op.From)
				for to := range adj[op.From] {
					delete(edges, Edge{op.From, to})
				}
				delete(adj, op.From)
				for from, set := range adj {
					if _, ok := set[op.From]; ok {
						delete(set, op.From)
						delete(edges, Edge{from, op.From})
					}
				}
			}
		case AddEdge:
			e := Edge{op.From, op.To}
			if _, ok := nodes[op.From]; !ok {
				err = ErrNotFound
			} else if _, ok := nodes[op.To]; !ok {
				err = ErrNotFound
			} else if _, ok := edges[e]; ok {
				err = ErrExists
			} else if reachable(adj, op.To, op.From) {
				err = ErrCycle
			} else {
				edges[e] = struct{}{}
				s := adj[op.From]
				if s == nil {
					s = make(map[string]struct{})
					adj[op.From] = s
				}
				s[op.To] = struct{}{}
			}
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := edges[e]; !ok {
				err = ErrNotFound
			} else {
				delete(edges, e)
				delete(adj[op.From], op.To)
			}
		}
		if err != nil {
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

// reachable reports whether target is reachable from start (including
// start == target) following out-edges. Caller must hold the lock.
func reachable(adj map[string]map[string]struct{}, start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	stack := []string{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for to := range adj[n] {
			if to == target {
				return true
			}
			if _, ok := seen[to]; !ok {
				seen[to] = struct{}{}
				stack = append(stack, to)
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
	return reachable(g.adj, from, to), nil
}

// Snapshot returns a consistent view with nodes and edges stably sorted.
// The returned slices are freshly allocated and isolated from the graph.
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
