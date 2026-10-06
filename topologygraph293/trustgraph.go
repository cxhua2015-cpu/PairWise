package topologygraph293

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

// Graph is a concurrency-safe in-memory control topology graph.
type Graph struct {
	mu    sync.RWMutex
	opts  Options
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
	gen   uint64
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

// validateOp performs structural validation of a single op without reading state.
func (g *Graph) validateOp(op Op) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" || !validName(op.From, g.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(op.From, g.opts.MaxNameBytes) || !validName(op.To, g.opts.MaxNameBytes) || op.From == op.To {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// Apply executes a batch atomically against the graph.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		defer g.mu.RUnlock()
		return Result{Generation: g.gen}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Candidate transaction: work on copies, commit only on success.
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

	reaches := func(from, to string) bool {
		if from == to {
			return true
		}
		seen := map[string]struct{}{from: {}}
		stack := []string{from}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for m := range out[n] {
				if m == to {
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
			for m := range out[op.From] {
				delEdge(Edge{op.From, m})
			}
			for m := range in[op.From] {
				delEdge(Edge{m, op.From})
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
			if reaches(op.To, op.From) {
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

	if len(nodes) > g.opts.MaxNodes || len(edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes, g.edges, g.out, g.in = nodes, edges, out, in
	g.gen++
	return Result{Generation: g.gen}, nil
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
	if from == to {
		return true, nil
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range g.out[n] {
			if m == to {
				return true, nil
			}
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				stack = append(stack, m)
			}
		}
	}
	return false, nil
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
