package topologygraph258

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
// All public methods are safe for concurrent use.
type Graph struct {
	mu         sync.RWMutex
	maxNodes   int
	maxEdges   int
	nameBytes  int
	generation uint64
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
}

// New validates the options and returns an empty graph.
func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxNodes:  o.MaxNodes,
		maxEdges:  o.MaxEdges,
		nameBytes: o.MaxNameBytes,
		nodes:     make(map[string]struct{}),
		edges:     make(map[Edge]struct{}),
	}, nil
}

// Apply validates the batch structurally, replays it against a candidate
// copy of the state, checks final capacity, and commits atomically.
// Any failure rolls the whole batch back and leaves the generation unchanged.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		res := Result{Generation: g.generation}
		g.mu.RUnlock()
		return res, nil
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
		nodes, edges, err = applyOp(nodes, edges, op)
		if err != nil {
			return Result{}, err
		}
	}
	if len(nodes) > g.maxNodes || len(edges) > g.maxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes = nodes
	g.edges = edges
	g.generation++
	return Result{Generation: g.generation}, nil
}

// applyOp applies a single structurally valid op to the candidate state.
func applyOp(nodes map[string]struct{}, edges map[Edge]struct{}, op Op) (map[string]struct{}, map[Edge]struct{}, error) {
	switch op.Kind {
	case AddNode:
		if _, ok := nodes[op.From]; ok {
			return nil, nil, ErrExists
		}
		nodes[op.From] = struct{}{}
	case DeleteNode:
		if _, ok := nodes[op.From]; !ok {
			return nil, nil, ErrNotFound
		}
		delete(nodes, op.From)
		for e := range edges {
			if e.From == op.From || e.To == op.From {
				delete(edges, e)
			}
		}
	case AddEdge:
		if _, ok := nodes[op.From]; !ok {
			return nil, nil, ErrNotFound
		}
		if _, ok := nodes[op.To]; !ok {
			return nil, nil, ErrNotFound
		}
		e := Edge{From: op.From, To: op.To}
		if _, ok := edges[e]; ok {
			return nil, nil, ErrExists
		}
		if reaches(edges, op.To, op.From) {
			return nil, nil, ErrCycle
		}
		edges[e] = struct{}{}
	case DeleteEdge:
		e := Edge{From: op.From, To: op.To}
		if _, ok := edges[e]; !ok {
			return nil, nil, ErrNotFound
		}
		delete(edges, e)
	}
	return nodes, edges, nil
}

// reaches reports whether dst is reachable from src following directed edges.
func reaches(edges map[Edge]struct{}, src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	queue := []string{src}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for e := range edges {
			if e.From != cur {
				continue
			}
			if e.To == dst {
				return true
			}
			if _, ok := seen[e.To]; !ok {
				seen[e.To] = struct{}{}
				queue = append(queue, e.To)
			}
		}
	}
	return false
}

// Reachable reports whether a directed path from src to dst exists using a
// consistent snapshot of the current state.
func (g *Graph) Reachable(src, dst string) (bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if !validName(src, g.nameBytes) || !validName(dst, g.nameBytes) {
		return false, ErrInvalidInput
	}
	if _, ok := g.nodes[src]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[dst]; !ok {
		return false, ErrNotFound
	}
	return reaches(g.edges, src, dst), nil
}

// Snapshot returns a consistently ordered, fully detached copy of the state.
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
