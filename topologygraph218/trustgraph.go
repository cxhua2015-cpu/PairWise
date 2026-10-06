package topologygraph218

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

func validName(opts Options, s string) bool {
	if s == "" || len(s) > opts.MaxNameBytes {
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

func New(opts Options) (*Graph, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  opts,
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		adj:   make(map[string]map[string]struct{}),
	}, nil
}

func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(g.opts, op.From) {
				return ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(g.opts, op.From) || !validName(g.opts, op.To) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// createsCycle reports whether target can reach source via current adjacency.
func (g *Graph) reachableLocked(from, to string) bool {
	if from == to {
		return true
	}
	visited := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range g.adj[n] {
			if next == to {
				return true
			}
			if _, ok := visited[next]; !ok {
				visited[next] = struct{}{}
				stack = append(stack, next)
			}
		}
	}
	return false
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.validate(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	type undo struct {
		kind     Kind
		from, to string
	}
	var log []undo
	rollback := func() {
		for i := len(log) - 1; i >= 0; i-- {
			u := log[i]
			switch u.kind {
			case AddNode:
				delete(g.nodes, u.from)
			case DeleteNode:
				g.nodes[u.from] = struct{}{}
			case AddEdge:
				g.removeEdgeLocked(u.from, u.to)
			case DeleteEdge:
				g.addEdgeLocked(u.from, u.to)
			}
		}
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := g.nodes[op.From]; ok {
				rollback()
				return Result{}, ErrExists
			}
			g.nodes[op.From] = struct{}{}
			log = append(log, undo{AddNode, op.From, ""})
		case DeleteNode:
			if _, ok := g.nodes[op.From]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			// remove incident edges, recording them for rollback
			var incident []Edge
			for e := range g.edges {
				if e.From == op.From || e.To == op.From {
					incident = append(incident, e)
				}
			}
			for _, e := range incident {
				g.removeEdgeLocked(e.From, e.To)
				log = append(log, undo{DeleteEdge, e.From, e.To})
			}
			delete(g.nodes, op.From)
			log = append(log, undo{DeleteNode, op.From, ""})
		case AddEdge:
			if _, ok := g.nodes[op.From]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			if _, ok := g.nodes[op.To]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			e := Edge{op.From, op.To}
			if _, ok := g.edges[e]; ok {
				rollback()
				return Result{}, ErrExists
			}
			if g.reachableLocked(op.To, op.From) {
				rollback()
				return Result{}, ErrCycle
			}
			g.addEdgeLocked(op.From, op.To)
			log = append(log, undo{AddEdge, op.From, op.To})
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := g.edges[e]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			g.removeEdgeLocked(op.From, op.To)
			log = append(log, undo{DeleteEdge, op.From, op.To})
		}
	}
	if len(g.nodes) > g.opts.MaxNodes || len(g.edges) > g.opts.MaxEdges {
		rollback()
		return Result{}, ErrCapacity
	}
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) addEdgeLocked(from, to string) {
	g.edges[Edge{from, to}] = struct{}{}
	if g.adj[from] == nil {
		g.adj[from] = make(map[string]struct{})
	}
	g.adj[from][to] = struct{}{}
}

func (g *Graph) removeEdgeLocked(from, to string) {
	delete(g.edges, Edge{from, to})
	if outs := g.adj[from]; outs != nil {
		delete(outs, to)
		if len(outs) == 0 {
			delete(g.adj, from)
		}
	}
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(g.opts, from) || !validName(g.opts, to) {
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
	return g.reachableLocked(from, to), nil
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
