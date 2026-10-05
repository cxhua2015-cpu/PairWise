package controlgraph103

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

// Graph is a concurrency-safe in-memory directed dependency graph.
// All reads and writes are serialized by a single RWMutex; Apply performs
// structural validation before observing any state and commits only after the
// whole batch is proven valid, so failed batches leave state untouched.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	generation uint64
	nodes      map[string]struct{}
	// out stores outgoing adjacency; edges is the authoritative edge set and
	// guarantees duplicate-edge rejection independent of map iteration.
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
	edges map[Edge]struct{}
}

func New(opts Options) (*Graph, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  opts,
		nodes: map[string]struct{}{},
		out:   map[string]map[string]struct{}{},
		in:    map[string]map[string]struct{}{},
		edges: map[Edge]struct{}{},
	}, nil
}

// validName enforces non-empty ASCII of lowercase letters, digits, '-' and '_'
// within the configured byte limit.
func (g *Graph) validName(name string) bool {
	if len(name) == 0 || len(name) > g.opts.MaxNameBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

// structurallyValid validates every op without reading graph state. All ops
// must pass before any state-dependent check runs (spec ordering).
func (g *Graph) structurallyValid(ops []Op) error {
	for _, op := range ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if !g.validName(op.From) || op.To != "" {
				return ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !g.validName(op.From) || !g.validName(op.To) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// reaches reports whether dst is reachable from src in adjacency map out,
// including src == dst.
func reaches(out map[string]map[string]struct{}, src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range out[cur] {
			if next == dst {
				return true
			}
			if _, ok := seen[next]; ok {
				continue
			}
			seen[next] = struct{}{}
			stack = append(stack, next)
		}
	}
	return false
}

func (g *Graph) Apply(batch Batch) (Result, error) {
	if err := g.structurallyValid(batch.Ops); err != nil {
		return Result{}, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Candidate transaction: operate on copies so any failure rolls back by
	// simply discarding them; the committed state is only touched on success.
	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	out := make(map[string]map[string]struct{}, len(g.out))
	for n, set := range g.out {
		cp := make(map[string]struct{}, len(set))
		for v := range set {
			cp[v] = struct{}{}
		}
		out[n] = cp
	}
	in := make(map[string]map[string]struct{}, len(g.in))
	for n, set := range g.in {
		cp := make(map[string]struct{}, len(set))
		for v := range set {
			cp[v] = struct{}{}
		}
		in[n] = cp
	}
	edges := make(map[Edge]struct{}, len(g.edges))
	for e := range g.edges {
		edges[e] = struct{}{}
	}

	addNode := func(n string) error {
		if _, ok := nodes[n]; ok {
			return ErrExists
		}
		nodes[n] = struct{}{}
		out[n] = map[string]struct{}{}
		in[n] = map[string]struct{}{}
		return nil
	}
	deleteNode := func(n string) error {
		if _, ok := nodes[n]; !ok {
			return ErrNotFound
		}
		delete(nodes, n)
		// Cascade: remove every incident edge.
		for v := range out[n] {
			delete(in[v], n)
			delete(edges, Edge{n, v})
		}
		for v := range in[n] {
			delete(out[v], n)
			delete(edges, Edge{v, n})
		}
		delete(out, n)
		delete(in, n)
		return nil
	}
	addEdge := func(from, to string) error {
		if _, ok := nodes[from]; !ok {
			return ErrNotFound
		}
		if _, ok := nodes[to]; !ok {
			return ErrNotFound
		}
		e := Edge{from, to}
		if _, ok := edges[e]; ok {
			return ErrExists
		}
		// An edge from->to closes a cycle iff to already reaches from.
		if reaches(out, to, from) {
			return ErrCycle
		}
		edges[e] = struct{}{}
		out[from][to] = struct{}{}
		in[to][from] = struct{}{}
		return nil
	}
	deleteEdge := func(from, to string) error {
		e := Edge{from, to}
		if _, ok := edges[e]; !ok {
			return ErrNotFound
		}
		delete(edges, e)
		if set, ok := out[from]; ok {
			delete(set, to)
		}
		if set, ok := in[to]; ok {
			delete(set, from)
		}
		return nil
	}

	for _, op := range batch.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = addNode(op.From)
		case DeleteNode:
			err = deleteNode(op.From)
		case AddEdge:
			err = addEdge(op.From, op.To)
		case DeleteEdge:
			err = deleteEdge(op.From, op.To)

		}
		if err != nil {
			// Discard the candidate transaction: committed maps are untouched.
			return Result{}, err
		}
	}

	// Capacity is checked against final state only, so an over-limit
	// intermediate state within one batch is legal.
	if len(nodes) > g.opts.MaxNodes || len(edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}

	if len(batch.Ops) > 0 {
		g.generation++
	}
	g.nodes = nodes
	g.out = out
	g.in = in
	g.edges = edges
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !g.validName(from) || !g.validName(to) {
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

// Snapshot returns a consistent, independently owned copy with nodes sorted
// lexicographically and edges sorted by (From, To).
func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()

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
