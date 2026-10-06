package topologygraph228

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
// The zero value is not usable; construct with New.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{}
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
		out:   make(map[string]map[string]struct{}),
	}, nil
}

// Apply validates the batch structurally, replays it against a candidate
// transaction, checks final capacity, and commits atomically.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
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
	out := make(map[string]map[string]struct{}, len(g.out))
	for from, tos := range g.out {
		s := make(map[string]struct{}, len(tos))
		for to := range tos {
			s[to] = struct{}{}
		}
		out[from] = s
	}

	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = applyAddNode(nodes, op.From)
		case DeleteNode:
			err = applyDeleteNode(nodes, edges, out, op.From)
		case AddEdge:
			err = applyAddEdge(nodes, edges, out, op.From, op.To)
		case DeleteEdge:
			err = applyDeleteEdge(edges, out, op.From, op.To)
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
	g.out = out
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

func applyDeleteNode(nodes map[string]struct{}, edges map[Edge]struct{}, out map[string]map[string]struct{}, name string) error {
	if _, ok := nodes[name]; !ok {
		return ErrNotFound
	}
	delete(nodes, name)
	for e := range edges {
		if e.From == name || e.To == name {
			delete(edges, e)
		}
	}
	delete(out, name)
	for from, tos := range out {
		if _, ok := tos[name]; ok {
			delete(tos, name)
			if len(tos) == 0 {
				delete(out, from)
			}
		}
	}
	return nil
}

func applyAddEdge(nodes map[string]struct{}, edges map[Edge]struct{}, out map[string]map[string]struct{}, from, to string) error {
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
	if reaches(out, to, from) {
		return ErrCycle
	}
	edges[e] = struct{}{}
	if out[from] == nil {
		out[from] = make(map[string]struct{})
	}
	out[from][to] = struct{}{}
	return nil
}

func applyDeleteEdge(edges map[Edge]struct{}, out map[string]map[string]struct{}, from, to string) error {
	e := Edge{From: from, To: to}
	if _, ok := edges[e]; !ok {
		return ErrNotFound
	}
	delete(edges, e)
	if tos, ok := out[from]; ok {
		delete(tos, to)
		if len(tos) == 0 {
			delete(out, from)
		}
	}
	return nil
}

// reaches reports whether dst is reachable from src along out edges.
func reaches(out map[string]map[string]struct{}, src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range out[n] {
			if next == dst {
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
	return reaches(g.out, from, to), nil
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
