package controlgraph178

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
	mu     sync.RWMutex
	nodes  map[string]struct{}
	edges  map[Edge]struct{}
	out    map[string]map[string]struct{}
	in     map[string]map[string]struct{}
	gen    uint64
	maxN   int
	maxE   int
	maxLen int
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		nodes:  make(map[string]struct{}),
		edges:  make(map[Edge]struct{}),
		out:    make(map[string]map[string]struct{}),
		in:     make(map[string]map[string]struct{}),
		maxN:   o.MaxNodes,
		maxE:   o.MaxEdges,
		maxLen: o.MaxNameBytes,
	}, nil
}

func validName(s string, maxLen int) bool {
	if s == "" || len(s) > maxLen {
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
	// 1. Full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, g.maxLen) {
				return Result{}, ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.maxLen) || !validName(op.To, g.maxLen) {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}

	// 2. Candidate transaction: clone state, mutate the clone.
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
		m := make(map[string]struct{}, len(v))
		for x := range v {
			m[x] = struct{}{}
		}
		out[k] = m
	}
	in := make(map[string]map[string]struct{}, len(g.in))
	for k, v := range g.in {
		m := make(map[string]struct{}, len(v))
		for x := range v {
			m[x] = struct{}{}
		}
		in[k] = m
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
			for to := range out[op.From] {
				delEdge(Edge{From: op.From, To: to})
			}
			for from := range in[op.From] {
				delEdge(Edge{From: from, To: op.From})
			}
			delete(nodes, op.From)
			delete(out, op.From)
			delete(in, op.From)
		case AddEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := edges[e]; ok {
				return Result{}, ErrExists
			}
			if reaches(out, op.To, op.From) {
				return Result{}, ErrCycle
			}
			addEdge(e)
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := edges[e]; !ok {
				return Result{}, ErrNotFound
			}
			delEdge(e)
		}
	}

	// 3. Final capacity check; failure discards the candidate.
	if len(nodes) > g.maxN || len(edges) > g.maxE {
		return Result{}, ErrCapacity
	}

	g.nodes, g.edges, g.out, g.in = nodes, edges, out, in
	g.gen++
	return Result{Generation: g.gen}, nil
}

// reaches reports whether dst is reachable from src (src == dst counts).
func reaches(out map[string]map[string]struct{}, src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range out[n] {
			if m == dst {
				return true
			}
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				stack = append(stack, m)
			}
		}
	}
	return false
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.maxLen) || !validName(to, g.maxLen) {
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
