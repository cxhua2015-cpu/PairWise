package topologygraph298

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
// A single RWMutex guards all state; writers apply atomic batches
// against a candidate state and only commit on success.
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

// Apply executes a batch atomically. The batch is fully validated
// structurally before any state is read, then applied to a candidate
// state; capacity limits are checked only at the end. Any failure
// discards the candidate, leaving the graph untouched.
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
		switch op.Kind {
		case AddNode:
			err = applyAddNode(nodes, op.From)
		case DeleteNode:
			err = applyDeleteNode(nodes, edges, op.From)
		case AddEdge:
			err = applyAddEdge(nodes, edges, Edge{op.From, op.To})
		case DeleteEdge:
			err = applyDeleteEdge(nodes, edges, Edge{op.From, op.To})
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
	g.generation++
	return Result{Generation: g.generation}, nil
}

func applyAddNode(nodes map[string]struct{}, n string) error {
	if _, ok := nodes[n]; ok {
		return ErrExists
	}
	nodes[n] = struct{}{}
	return nil
}

func applyDeleteNode(nodes map[string]struct{}, edges map[Edge]struct{}, n string) error {
	if _, ok := nodes[n]; !ok {
		return ErrNotFound
	}
	delete(nodes, n)
	for e := range edges {
		if e.From == n || e.To == n {
			delete(edges, e)
		}
	}
	return nil
}

func applyAddEdge(nodes map[string]struct{}, edges map[Edge]struct{}, e Edge) error {
	if _, ok := nodes[e.From]; !ok {
		return ErrNotFound
	}
	if _, ok := nodes[e.To]; !ok {
		return ErrNotFound
	}
	if _, ok := edges[e]; ok {
		return ErrExists
	}
	if reaches(edges, e.To, e.From) {
		return ErrCycle
	}
	edges[e] = struct{}{}
	return nil
}

func applyDeleteEdge(nodes map[string]struct{}, edges map[Edge]struct{}, e Edge) error {
	if _, ok := nodes[e.From]; !ok {
		return ErrNotFound
	}
	if _, ok := nodes[e.To]; !ok {
		return ErrNotFound
	}
	if _, ok := edges[e]; !ok {
		return ErrNotFound
	}
	delete(edges, e)
	return nil
}

// reaches reports whether target is reachable from start over edges.
func reaches(edges map[Edge]struct{}, start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for e := range edges {
			if e.From != cur {
				continue
			}
			if e.To == target {
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

// Reachable reports whether a directed path from->to exists using a
// consistent snapshot of the current committed state.
func (g *Graph) Reachable(from, to string) (bool, error) {
	if err := validateName(from, g.opts.MaxNameBytes); err != nil {
		return false, err
	}
	if err := validateName(to, g.opts.MaxNameBytes); err != nil {
		return false, err
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return false, ErrNotFound
	}
	return reaches(g.edges, from, to), nil
}

// Snapshot returns a consistent, stably sorted copy of the state.
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
