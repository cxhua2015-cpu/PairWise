package topologygraph298

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
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	adj        map[string]map[string]struct{}
	generation uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  o,
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		adj:   make(map[string]map[string]struct{}),
	}, nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		defer g.mu.RUnlock()
		return Result{Generation: g.generation}, nil
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
	adj := make(map[string]map[string]struct{}, len(g.adj))
	for from, set := range g.adj {
		s := make(map[string]struct{}, len(set))
		for to := range set {
			s[to] = struct{}{}
		}
		adj[from] = s
	}

	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			if _, ok := nodes[op.From]; ok {
				err = ErrExists
			} else {
				nodes[op.From] = struct{}{}
			}
		case DeleteNode:
			if _, ok := nodes[op.From]; !ok {
				err = ErrNotFound
			} else {
				delete(nodes, op.From)
				for to := range adj[op.From] {
					delete(edges, Edge{op.From, to})
				}
				delete(adj, op.From)
				for from, set := range adj {
					if _, ok := set[op.From]; ok {
						delete(set, op.From)
						delete(edges, Edge{from, op.From})
					}
				}
			}
		case AddEdge:
			if _, ok := nodes[op.From]; !ok {
				err = ErrNotFound
			} else if _, ok := nodes[op.To]; !ok {
				err = ErrNotFound
			} else if _, ok := edges[Edge{op.From, op.To}]; ok {
				err = ErrExists
			} else if reaches(adj, op.To, op.From) {
				err = ErrCycle
			} else {
				edges[Edge{op.From, op.To}] = struct{}{}
				if adj[op.From] == nil {
					adj[op.From] = make(map[string]struct{})
				}
				adj[op.From][op.To] = struct{}{}
			}
		case DeleteEdge:
			if _, ok := edges[Edge{op.From, op.To}]; !ok {
				err = ErrNotFound
			} else {
				delete(edges, Edge{op.From, op.To})
				delete(adj[op.From], op.To)
			}
		}
		if err != nil {
			return Result{}, err
		}
	}

	if len(nodes) > g.opts.MaxNodes || len(edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes, g.edges, g.adj = nodes, edges, adj
	g.generation++
	return Result{Generation: g.generation}, nil
}

func reaches(adj map[string]map[string]struct{}, from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range adj[n] {
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
	if err := validName(from, g.opts.MaxNameBytes); err != nil {
		return false, err
	}
	if err := validName(to, g.opts.MaxNameBytes); err != nil {
		return false, err
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return false, ErrNotFound
	}
	return reaches(g.adj, from, to), nil
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
