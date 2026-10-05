package replicationgraph

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

// validateOp performs structural validation only; it must not read graph state.
func (g *Graph) validateOp(op Op) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" || !validName(op.From, g.maxNameLen) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(op.From, g.maxNameLen) || !validName(op.To, g.maxNameLen) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	// Structural validation of the whole batch before touching state.
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}

	// Candidate transaction: clone state, mutate the clone, commit on success.
	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	edges := make(map[Edge]struct{}, len(g.edges))
	for e := range g.edges {
		edges[e] = struct{}{}
	}
	adj := make(map[string]map[string]struct{}, len(g.adj))
	for from, set := range g.adj {
		s := make(map[string]struct{}, len(set))
		for to := range set {
			s[to] = struct{}{}
		}
		adj[from] = s
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
			// Remove all incident edges.
			for to := range adj[op.From] {
				delete(edges, Edge{From: op.From, To: to})
			}
			delete(adj, op.From)
			for from, set := range adj {
				if _, ok := set[op.From]; ok {
					delete(set, op.From)
					delete(edges, Edge{From: from, To: op.From})
				}
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
			if reachable(adj, op.To, op.From) {
				return Result{}, ErrCycle
			}
			edges[e] = struct{}{}
			set := adj[op.From]
			if set == nil {
				set = make(map[string]struct{})
				adj[op.From] = set
			}
			set[op.To] = struct{}{}
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
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

	g.nodes = nodes
	g.edges = edges
	g.adj = adj
	g.generation++
	return Result{Generation: g.generation}, nil
}

// reachable reports whether dst is reachable from src via adj (src == dst counts).
func reachable(adj map[string]map[string]struct{}, src, dst string) bool {
	if src == dst {
		return true
	}
	visited := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for to := range adj[n] {
			if to == dst {
				return true
			}
			if _, ok := visited[to]; !ok {
				visited[to] = struct{}{}
				stack = append(stack, to)
			}
		}
	}
	return false
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.maxNameLen) || !validName(to, g.maxNameLen) {
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
