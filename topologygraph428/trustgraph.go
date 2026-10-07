package topologygraph428

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

// applyOps replays a structurally valid batch against the given state,
// enforcing semantic rules in operation order. It mutates nodes/edges in
// place; callers pass candidate maps so failures roll back by discard.
func (g *Graph) applyOps(nodes map[string]struct{}, edges map[Edge]struct{}, ops []Op) error {
	for _, op := range ops {
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
			if reachable(edges, op.To, op.From) {
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
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.validateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	nodes, edges := g.copyStateLocked()
	if err := g.applyOps(nodes, edges, b.Ops); err != nil {
		return Result{}, err
	}
	if len(nodes) > g.opts.MaxNodes || len(edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
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
	return reachable(g.edges, from, to), nil
}

func reachable(edges map[Edge]struct{}, from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	queue := []string{from}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for e := range edges {
			if e.From != n {
				continue
			}
			if e.To == to {
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

func (g *Graph) copyStateLocked() (map[string]struct{}, map[Edge]struct{}) {
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
