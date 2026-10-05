package trustgraph

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
	maxNodes   int
	maxEdges   int
	maxName    int
	generation uint64
	nodes      map[string]struct{}
	// out[x] holds the set of nodes y with an edge x -> y.
	out map[string]map[string]struct{}
	// in[y] holds the set of nodes x with an edge x -> y.
	in map[string]map[string]struct{}
	edges int
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxNodes: o.MaxNodes,
		maxEdges: o.MaxEdges,
		maxName:  o.MaxNameBytes,
		nodes:    make(map[string]struct{}),
		out:      make(map[string]map[string]struct{}),
		in:       make(map[string]map[string]struct{}),
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

func (g *Graph) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching any state.
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, g.maxName) {
				return Result{}, ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.maxName) || !validName(op.To, g.maxName) {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}

	// Phase 2: candidate transaction on copies; commit only on success.
	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
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
	edges := g.edges

	addEdge := func(from, to string) {
		if out[from] == nil {
			out[from] = make(map[string]struct{})
		}
		out[from][to] = struct{}{}
		if in[to] == nil {
			in[to] = make(map[string]struct{})
		}
		in[to][from] = struct{}{}
		edges++
	}
	removeEdge := func(from, to string) {
		delete(out[from], to)
		if len(out[from]) == 0 {
			delete(out, from)
		}
		delete(in[to], from)
		if len(in[to]) == 0 {
			delete(in, to)
		}
		edges--
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
			delete(nodes, op.From)
			for to := range out[op.From] {
				removeEdge(op.From, to)
			}
			for from := range in[op.From] {
				removeEdge(from, op.From)
			}
		case AddEdge:
			if _, ok := nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := out[op.From][op.To]; ok {
				return Result{}, ErrExists
			}
			if reachable(out, op.To, op.From) {
				return Result{}, ErrCycle
			}
			addEdge(op.From, op.To)
		case DeleteEdge:
			if _, ok := out[op.From][op.To]; !ok {
				return Result{}, ErrNotFound
			}
			removeEdge(op.From, op.To)
		}
	}

	// Capacity is only enforced on the final state of the batch.
	if len(nodes) > g.maxNodes || edges > g.maxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes = nodes
	g.out = out
	g.in = in
	g.edges = edges
	g.generation++
	return Result{Generation: g.generation}, nil
}

// reachable reports whether dst is reachable from src following out edges.
func reachable(out map[string]map[string]struct{}, src, dst string) bool {
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
	return reachable(g.out, from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := Snapshot{
		Generation: g.generation,
		Nodes:      make([]string, 0, len(g.nodes)),
		Edges:      make([]Edge, 0, g.edges),
	}
	for n := range g.nodes {
		s.Nodes = append(s.Nodes, n)
	}
	sort.Strings(s.Nodes)
	for from, tos := range g.out {
		for to := range tos {
			s.Edges = append(s.Edges, Edge{From: from, To: to})
		}
	}
	sort.Slice(s.Edges, func(i, j int) bool {
		if s.Edges[i].From != s.Edges[j].From {
			return s.Edges[i].From < s.Edges[j].From
		}
		return s.Edges[i].To < s.Edges[j].To
	})
	return s
}
