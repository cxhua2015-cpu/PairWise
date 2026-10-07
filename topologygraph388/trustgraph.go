// Package topologygraph388 implements a concurrency-safe in-memory
// directed control-topology graph with atomic batch mutations.
package topologygraph388

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

// Graph is a concurrency-safe in-memory directed graph. The zero value
// is not usable; construct one with New.
type Graph struct {
	mu      sync.RWMutex
	maxNode int
	maxEdge int
	maxName int
	nodes   map[string]struct{}
	edges   map[Edge]struct{}
	out     map[string]map[string]struct{} // adjacency: from -> set of to
	in      map[string]map[string]struct{} // reverse adjacency
	gen     uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxNode: o.MaxNodes,
		maxEdge: o.MaxEdges,
		maxName: o.MaxNameBytes,
		nodes:   make(map[string]struct{}),
		edges:   make(map[Edge]struct{}),
		out:     make(map[string]map[string]struct{}),
		in:      make(map[string]map[string]struct{}),
	}, nil
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
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

// validate checks the batch structurally without reading graph state.
func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, g.maxName) {
				return ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.maxName) || !validName(op.To, g.maxName) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
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
		return Result{Generation: g.gen}, nil
	}

	// Candidate transaction: clone state, apply ops, commit on success.
	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	edges := make(map[Edge]struct{}, len(g.edges))
	for e := range g.edges {
		edges[e] = struct{}{}
	}
	out := make(map[string]map[string]struct{}, len(g.out))
	for k, v := range g.out {
		s := make(map[string]struct{}, len(v))
		for x := range v {
			s[x] = struct{}{}
		}
		out[k] = s
	}
	in := make(map[string]map[string]struct{}, len(g.in))
	for k, v := range g.in {
		s := make(map[string]struct{}, len(v))
		for x := range v {
			s[x] = struct{}{}
		}
		in[k] = s
	}

	addEdge := func(e Edge) {
		edges[e] = struct{}{}
		if out[e.From] == nil {
			out[e.From] = make(map[string]struct{})
		}
		out[e.From][e.To] = struct{}{}
		if in[e.To] == nil {
			in[e.To] = make(map[string]struct{})
		}
		in[e.To][e.From] = struct{}{}
	}
	delEdge := func(e Edge) {
		delete(edges, e)
		delete(out[e.From], e.To)
		delete(in[e.To], e.From)
	}
	delNode := func(n string) {
		for to := range out[n] {
			delete(edges, Edge{n, to})
			delete(in[to], n)
		}
		for from := range in[n] {
			delete(edges, Edge{from, n})
			delete(out[from], n)
		}
		delete(out, n)
		delete(in, n)
		delete(nodes, n)
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := nodes[op.From]; ok {
				return Result{}, ErrExists
			}
			nodes[op.From] = struct{}{}
		case DeleteNode:
			if _, ok := nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			delNode(op.From)
		case AddEdge:
			e := Edge{op.From, op.To}
			if _, ok := nodes[e.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := nodes[e.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := edges[e]; ok {
				return Result{}, ErrExists
			}
			if e.From == e.To || reachable(out, e.To, e.From) {
				return Result{}, ErrCycle
			}
			addEdge(e)
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := edges[e]; !ok {
				return Result{}, ErrNotFound
			}
			delEdge(e)
		}
	}

	if len(nodes) > g.maxNode || len(edges) > g.maxEdge {
		return Result{}, ErrCapacity
	}

	g.nodes, g.edges, g.out, g.in = nodes, edges, out, in
	g.gen++
	return Result{Generation: g.gen}, nil
}

// reachable reports whether dst is reachable from src via out edges (BFS).
func reachable(out map[string]map[string]struct{}, src, dst string) bool {
	seen := map[string]struct{}{src: {}}
	queue := []string{src}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for to := range out[n] {
			if to == dst {
				return true
			}
			if _, ok := seen[to]; !ok {
				seen[to] = struct{}{}
				queue = append(queue, to)
			}
		}
	}
	return false
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.maxName) || !validName(to, g.maxName) {
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
	if from == to {
		return true, nil
	}
	return reachable(g.out, from, to), nil
}

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
