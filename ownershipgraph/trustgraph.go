// Package ownershipgraph implements a concurrency-safe, in-memory
// ownership dependency graph with atomic batches.
package ownershipgraph

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

type state struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

// Graph is a concurrency-safe ownership dependency graph.
// All public methods may be called concurrently.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	st         state
	generation uint64
}

// New creates an empty Graph. All limits must be positive.
func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts: o,
		st: state{
			nodes: make(map[string]struct{}),
			edges: make(map[Edge]struct{}),
			out:   make(map[string]map[string]struct{}),
			in:    make(map[string]map[string]struct{}),
		},
	}, nil
}

func validName(s string, maxBytes int) bool {
	if s == "" || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validate performs full structural validation of a batch without
// reading any graph state.
func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" {
				return ErrInvalidInput
			}
			if !validName(op.From, g.opts.MaxNameBytes) {
				return ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.opts.MaxNameBytes) || !validName(op.To, g.opts.MaxNameBytes) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// clone returns a deep copy of s, used as the candidate transaction state.
func (s state) clone() state {
	c := state{
		nodes: make(map[string]struct{}, len(s.nodes)),
		edges: make(map[Edge]struct{}, len(s.edges)),
		out:   make(map[string]map[string]struct{}, len(s.out)),
		in:    make(map[string]map[string]struct{}, len(s.in)),
	}
	for k := range s.nodes {
		c.nodes[k] = struct{}{}
	}
	for k := range s.edges {
		c.edges[k] = struct{}{}
	}
	for k, v := range s.out {
		m := make(map[string]struct{}, len(v))
		for x := range v {
			m[x] = struct{}{}
		}
		c.out[k] = m
	}
	for k, v := range s.in {
		m := make(map[string]struct{}, len(v))
		for x := range v {
			m[x] = struct{}{}
		}
		c.in[k] = m
	}
	return c
}

func (s state) addEdge(from, to string) {
	s.edges[Edge{from, to}] = struct{}{}
	if s.out[from] == nil {
		s.out[from] = make(map[string]struct{})
	}
	s.out[from][to] = struct{}{}
	if s.in[to] == nil {
		s.in[to] = make(map[string]struct{})
	}
	s.in[to][from] = struct{}{}
}

func (s state) removeEdge(from, to string) {
	delete(s.edges, Edge{from, to})
	if m := s.out[from]; m != nil {
		delete(m, to)
		if len(m) == 0 {
			delete(s.out, from)
		}
	}
	if m := s.in[to]; m != nil {
		delete(m, from)
		if len(m) == 0 {
			delete(s.in, to)
		}
	}
}

// reachable reports whether dst is reachable from src following out-edges.
func (s state) reachable(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	queue := []string{src}
	for len(queue) > 0 {
		cur := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for next := range s.out[cur] {
			if next == dst {
				return true
			}
			if _, ok := seen[next]; !ok {
				seen[next] = struct{}{}
				queue = append(queue, next)
			}
		}
	}
	return false
}

// Apply atomically applies a batch of operations. The batch is fully
// validated structurally before any state is read; on any error the
// graph is left unchanged. Capacity limits are checked only against the
// final state of the batch. A non-empty successful batch increments the
// generation exactly once.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.validate(b); err != nil {
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

	cand := g.st.clone()
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := cand.nodes[op.From]; ok {
				return Result{}, ErrExists
			}
			cand.nodes[op.From] = struct{}{}
		case DeleteNode:
			if _, ok := cand.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			for to := range cand.out[op.From] {
				cand.removeEdge(op.From, to)
			}
			for from := range cand.in[op.From] {
				cand.removeEdge(from, op.From)
			}
			delete(cand.nodes, op.From)
		case AddEdge:
			if _, ok := cand.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.edges[Edge{op.From, op.To}]; ok {
				return Result{}, ErrExists
			}
			if cand.reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			cand.addEdge(op.From, op.To)
		case DeleteEdge:
			if _, ok := cand.edges[Edge{op.From, op.To}]; !ok {
				return Result{}, ErrNotFound
			}
			cand.removeEdge(op.From, op.To)
		}
	}

	if len(cand.nodes) > g.opts.MaxNodes || len(cand.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}

	g.st = cand
	g.generation++
	return Result{Generation: g.generation}, nil
}

// Reachable reports whether dst is reachable from dst's perspective of a
// consistent snapshot of the current graph: it returns true if there is a
// directed path from src to dst.
func (g *Graph) Reachable(src, dst string) (bool, error) {
	if !validName(src, g.opts.MaxNameBytes) || !validName(dst, g.opts.MaxNameBytes) {
		return false, ErrInvalidInput
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.st.nodes[src]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.st.nodes[dst]; !ok {
		return false, ErrNotFound
	}
	return g.st.reachable(src, dst), nil
}

// Snapshot returns a consistent, deterministically ordered copy of the
// graph. Nodes are sorted lexicographically; edges are sorted by From
// then To. The returned slices are independent of internal state.
func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	snap := Snapshot{
		Generation: g.generation,
		Nodes:      make([]string, 0, len(g.st.nodes)),
		Edges:      make([]Edge, 0, len(g.st.edges)),
	}
	for n := range g.st.nodes {
		snap.Nodes = append(snap.Nodes, n)
	}
	for e := range g.st.edges {
		snap.Edges = append(snap.Edges, e)
	}
	sort.Strings(snap.Nodes)
	sort.Slice(snap.Edges, func(i, j int) bool {
		if snap.Edges[i].From != snap.Edges[j].From {
			return snap.Edges[i].From < snap.Edges[j].From
		}
		return snap.Edges[i].To < snap.Edges[j].To
	})
	return snap
}
