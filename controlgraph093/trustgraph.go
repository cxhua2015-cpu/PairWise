package controlgraph093

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
		maxNodes: o.MaxNodes,
		maxEdges: o.MaxEdges,
		maxName:  o.MaxNameBytes,
		nodes:    make(map[string]struct{}),
		edges:    make(map[Edge]struct{}),
		adj:      make(map[string]map[string]struct{}),
	}, nil
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
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
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	type undo struct {
		kind Kind
		from, to string
	}
	var undos []undo
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			switch u.kind {
			case AddNode:
				g.removeNode(u.from)
			case DeleteNode:
				g.addNode(u.from)
			case AddEdge:
				g.removeEdge(u.from, u.to)
			case DeleteEdge:
				g.addEdge(u.from, u.to)
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
			g.addNode(op.From)
			undos = append(undos, undo{AddNode, op.From, ""})
		case DeleteNode:
			if _, ok := g.nodes[op.From]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			var removed []Edge
			for e := range g.edges {
				if e.From == op.From || e.To == op.From {
					removed = append(removed, e)
				}
			}
			sort.Slice(removed, func(i, j int) bool {
				if removed[i].From != removed[j].From {
					return removed[i].From < removed[j].From
				}
				return removed[i].To < removed[j].To
			})
			for _, e := range removed {
				g.removeEdge(e.From, e.To)
				undos = append(undos, undo{DeleteEdge, e.From, e.To})
			}
			g.removeNode(op.From)
			undos = append(undos, undo{DeleteNode, op.From, ""})
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
			g.addEdge(op.From, op.To)
			undos = append(undos, undo{AddEdge, op.From, op.To})
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := g.edges[e]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			g.removeEdge(op.From, op.To)
			undos = append(undos, undo{DeleteEdge, op.From, op.To})
		}
	}
	if len(g.nodes) > g.maxNodes || len(g.edges) > g.maxEdges {
		rollback()
		return Result{}, ErrCapacity
	}
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) addNode(n string) {
	g.nodes[n] = struct{}{}
}

func (g *Graph) removeNode(n string) {
	delete(g.nodes, n)
}

func (g *Graph) addEdge(from, to string) {
	g.edges[Edge{from, to}] = struct{}{}
	if g.adj[from] == nil {
		g.adj[from] = make(map[string]struct{})
	}
	g.adj[from][to] = struct{}{}
}

func (g *Graph) removeEdge(from, to string) {
	delete(g.edges, Edge{from, to})
	if m := g.adj[from]; m != nil {
		delete(m, to)
		if len(m) == 0 {
			delete(g.adj, from)
		}
	}
}

func (g *Graph) reachableLocked(from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range g.adj[n] {
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
	return g.reachableLocked(from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := Snapshot{Generation: g.generation}
	if len(g.nodes) > 0 {
		s.Nodes = make([]string, 0, len(g.nodes))
		for n := range g.nodes {
			s.Nodes = append(s.Nodes, n)
		}
		sort.Strings(s.Nodes)
	}
	if len(g.edges) > 0 {
		s.Edges = make([]Edge, 0, len(g.edges))
		for e := range g.edges {
			s.Edges = append(s.Edges, e)
		}
		sort.Slice(s.Edges, func(i, j int) bool {
			if s.Edges[i].From != s.Edges[j].From {
				return s.Edges[i].From < s.Edges[j].From
			}
			return s.Edges[i].To < s.Edges[j].To
		})
	}
	return s
}
