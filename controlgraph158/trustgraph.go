package controlgraph158

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
	maxNameLen int
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	adj        map[string]map[string]struct{} // from -> set of to
	generation uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxNodes:   o.MaxNodes,
		maxEdges:   o.MaxEdges,
		maxNameLen: o.MaxNameBytes,
		nodes:      make(map[string]struct{}),
		edges:      make(map[Edge]struct{}),
		adj:        make(map[string]map[string]struct{}),
	}, nil
}

func (g *Graph) validName(s string) bool {
	if s == "" || len(s) > g.maxNameLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validate checks the whole batch structurally before any state is read.
func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !g.validName(op.From) {
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

func (g *Graph) Apply(b Batch) (Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.validate(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	// Candidate transaction: clone state, apply ops, commit on success.
	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	edges := make(map[Edge]struct{}, len(g.edges))
	adj := make(map[string]map[string]struct{}, len(g.adj))
	for f, tos := range g.adj {
		s := make(map[string]struct{}, len(tos))
		for t := range tos {
			s[t] = struct{}{}
		}
		adj[f] = s
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
			for t := range adj[op.From] {
				delete(edges, Edge{op.From, t})
			}
			delete(adj, op.From)
			for f, tos := range adj {
				if _, ok := tos[op.From]; ok {
					delete(tos, op.From)
					delete(edges, Edge{f, op.From})
				}
			}
		case AddEdge:
			if _, ok := nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			e := Edge{op.From, op.To}
			if _, ok := edges[e]; ok {
				return Result{}, ErrExists
			}
			if reachable(adj, op.To, op.From) {
				return Result{}, ErrCycle
			}
			edges[e] = struct{}{}
			if adj[op.From] == nil {
				adj[op.From] = make(map[string]struct{})
			}
			adj[op.From][op.To] = struct{}{}
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := edges[e]; !ok {
				return Result{}, ErrNotFound
			}
			delete(edges, e)
			delete(adj[op.From], op.To)
		}
	}
	if len(nodes) > g.maxNodes || len(edges) > g.maxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges, g.adj = nodes, edges, adj
	g.generation++
	return Result{Generation: g.generation}, nil
}

// reachable reports whether dst is reachable from src in the given adjacency
// (src == dst counts as reachable, so a self-loop is a cycle).
func reachable(adj map[string]map[string]struct{}, src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for t := range adj[n] {
			if t == dst {
				return true
			}
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				stack = append(stack, t)
			}
		}
	}
	return false
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if !g.validName(from) || !g.validName(to) {
		return false, ErrInvalidInput
	}
	if _, ok := g.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return false, ErrNotFound
	}
	return reachable(g.adj, from, to), nil
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
