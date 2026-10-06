package topologygraph233

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
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
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
		in:    make(map[string]map[string]struct{}),
	}, nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		gen := g.generation
		g.mu.RUnlock()
		return Result{Generation: gen}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Candidate transaction: stage mutations so any failure rolls back cleanly.
	type undo struct{ apply func() }
	var undos []undo
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			undos[i].apply()
		}
	}

	addNode := func(n string) {
		g.nodes[n] = struct{}{}
		undos = append(undos, undo{func() { delete(g.nodes, n) }})
	}
	removeNode := func(n string) {
		delete(g.nodes, n)
		undos = append(undos, undo{func() { g.nodes[n] = struct{}{} }})
	}
	addEdge := func(e Edge) {
		g.edges[e] = struct{}{}
		if g.out[e.From] == nil {
			g.out[e.From] = make(map[string]struct{})
		}
		g.out[e.From][e.To] = struct{}{}
		if g.in[e.To] == nil {
			g.in[e.To] = make(map[string]struct{})
		}
		g.in[e.To][e.From] = struct{}{}
		undos = append(undos, undo{func() { g.removeEdge(e) }})
	}
	removeEdge := func(e Edge) {
		g.removeEdge(e)
		undos = append(undos, undo{func() {
			g.edges[e] = struct{}{}
			if g.out[e.From] == nil {
				g.out[e.From] = make(map[string]struct{})
			}
			g.out[e.From][e.To] = struct{}{}
			if g.in[e.To] == nil {
				g.in[e.To] = make(map[string]struct{})
			}
			g.in[e.To][e.From] = struct{}{}
		}})
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := g.nodes[op.From]; ok {
				rollback()
				return Result{}, ErrExists
			}
			addNode(op.From)
		case DeleteNode:
			if _, ok := g.nodes[op.From]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			// Cascade: remove all incident edges first (recorded for undo).
			for to := range g.out[op.From] {
				removeEdge(Edge{op.From, to})
			}
			for from := range g.in[op.From] {
				removeEdge(Edge{from, op.From})
			}
			removeNode(op.From)
		case AddEdge:
			e := Edge{op.From, op.To}
			if _, ok := g.nodes[op.From]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			if _, ok := g.nodes[op.To]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			if _, ok := g.edges[e]; ok {
				rollback()
				return Result{}, ErrExists
			}
			if g.reachableLocked(op.To, op.From) {
				rollback()
				return Result{}, ErrCycle
			}
			addEdge(e)
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := g.edges[e]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			removeEdge(e)
		}
	}

	// Capacity is only enforced on the final state of the batch.
	if len(g.nodes) > g.opts.MaxNodes || len(g.edges) > g.opts.MaxEdges {
		rollback()
		return Result{}, ErrCapacity
	}

	g.generation++
	return Result{Generation: g.generation}, nil
}

// removeEdge deletes an edge and prunes empty adjacency buckets.
func (g *Graph) removeEdge(e Edge) {
	delete(g.edges, e)
	if m := g.out[e.From]; m != nil {
		delete(m, e.To)
		if len(m) == 0 {
			delete(g.out, e.From)
		}
	}
	if m := g.in[e.To]; m != nil {
		delete(m, e.From)
		if len(m) == 0 {
			delete(g.in, e.To)
		}
	}
}

// reachableLocked reports whether dst is reachable from src. Caller holds the lock.
func (g *Graph) reachableLocked(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for to := range g.out[n] {
			if to == dst {
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
	return g.reachableLocked(from, to), nil
}

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
