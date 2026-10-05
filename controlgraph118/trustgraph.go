package controlgraph118

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
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
	generation uint64
	maxNodes   int
	maxEdges   int
	maxName    int
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		nodes:    make(map[string]struct{}),
		edges:    make(map[Edge]struct{}),
		out:      make(map[string]map[string]struct{}),
		in:       make(map[string]map[string]struct{}),
		maxNodes: o.MaxNodes,
		maxEdges: o.MaxEdges,
		maxName:  o.MaxNameBytes,
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
	if len(b.Ops) == 0 {
		g.mu.RLock()
		r := Result{Generation: g.generation}
		g.mu.RUnlock()
		return r, nil
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
		if len(out[e.From]) == 0 {
			delete(out, e.From)
		}
		delete(in[e.To], e.From)
		if len(in[e.To]) == 0 {
			delete(in, e.To)
		}
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
				delEdge(Edge{op.From, to})
			}
			for from := range in[op.From] {
				delEdge(Edge{from, op.From})
			}
		case AddEdge:
			e := Edge{op.From, op.To}
			if _, ok := nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := edges[e]; ok {
				return Result{}, ErrExists
			}
			if reachable(out, op.To, op.From) {
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

	if len(nodes) > g.maxNodes || len(edges) > g.maxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes = nodes
	g.edges = edges
	g.out = out
	g.in = in
	g.generation++
	return Result{Generation: g.generation}, nil
}

func reachable(out map[string]map[string]struct{}, from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range out[n] {
			if next == to {
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
