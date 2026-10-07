package topologygraph303

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
	mu      sync.RWMutex
	opts    Options
	gen     uint64
	nodes   map[string]struct{}
	edges   map[Edge]struct{}
	adj     map[string]map[string]struct{} // from -> set of to
	reverse map[string]map[string]struct{} // to -> set of from
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:    o,
		nodes:   make(map[string]struct{}),
		edges:   make(map[Edge]struct{}),
		adj:     make(map[string]map[string]struct{}),
		reverse: make(map[string]map[string]struct{}),
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

// validateOp checks only structural properties of a single op, without
// reading any graph state.
func (g *Graph) validateOp(op Op) error {
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
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Phase 1: structural validation of the whole batch, no state reads.
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}

	// Phase 2: stage a candidate transaction on cloned state.
	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	edges := make(map[Edge]struct{}, len(g.edges))
	for e := range g.edges {
		edges[e] = struct{}{}
	}
	adj := make(map[string]map[string]struct{}, len(g.adj))
	for k, v := range g.adj {
		s := make(map[string]struct{}, len(v))
		for x := range v {
			s[x] = struct{}{}
		}
		adj[k] = s
	}
	reverse := make(map[string]map[string]struct{}, len(g.reverse))
	for k, v := range g.reverse {
		s := make(map[string]struct{}, len(v))
		for x := range v {
			s[x] = struct{}{}
		}
		reverse[k] = s
	}

	addEdge := func(e Edge) {
		edges[e] = struct{}{}
		if adj[e.From] == nil {
			adj[e.From] = make(map[string]struct{})
		}
		adj[e.From][e.To] = struct{}{}
		if reverse[e.To] == nil {
			reverse[e.To] = make(map[string]struct{})
		}
		reverse[e.To][e.From] = struct{}{}
	}
	removeEdge := func(e Edge) {
		delete(edges, e)
		delete(adj[e.From], e.To)
		if len(adj[e.From]) == 0 {
			delete(adj, e.From)
		}
		delete(reverse[e.To], e.From)
		if len(reverse[e.To]) == 0 {
			delete(reverse, e.To)
		}
	}

	// reaches reports whether dst is reachable from src in the staged graph.
	reaches := func(src, dst string) bool {
		if src == dst {
			return true
		}
		seen := map[string]struct{}{src: {}}
		stack := []string{src}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for next := range adj[cur] {
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
			for to := range adj[op.From] {
				removeEdge(Edge{From: op.From, To: to})
			}
			for from := range reverse[op.From] {
				removeEdge(Edge{From: from, To: op.From})
			}
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
			if reaches(op.To, op.From) {
				return Result{}, ErrCycle
			}
			addEdge(e)
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := edges[e]; !ok {
				return Result{}, ErrNotFound
			}
			removeEdge(e)
		}
	}

	// Phase 3: capacity check only at batch end; failure rolls back entirely.
	if len(nodes) > g.opts.MaxNodes || len(edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes, g.edges, g.adj, g.reverse = nodes, edges, adj, reverse
	g.gen++
	return Result{Generation: g.gen}, nil
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
	if from == to {
		return true, nil
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range g.adj[cur] {
			if next == to {
				return true, nil
			}
			if _, ok := seen[next]; !ok {
				seen[next] = struct{}{}
				stack = append(stack, next)
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
