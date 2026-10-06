package topologygraph233

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
	adj        map[string]map[string]struct{}
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

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		res := Result{Generation: g.generation}
		g.mu.RUnlock()
		return res, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var undo []func()
	rollback := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = g.addNode(op.From, &undo)
		case DeleteNode:
			err = g.deleteNode(op.From, &undo)
		case AddEdge:
			err = g.addEdge(op.From, op.To, &undo)
		case DeleteEdge:
			err = g.deleteEdge(op.From, op.To, &undo)
		}
		if err != nil {
			rollback()
			return Result{}, err
		}
	}
	if len(g.nodes) > g.maxNodes || len(g.edges) > g.maxEdges {
		rollback()
		return Result{}, ErrCapacity
	}
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) addNode(n string, undo *[]func()) error {
	if _, ok := g.nodes[n]; ok {
		return ErrExists
	}
	g.nodes[n] = struct{}{}
	*undo = append(*undo, func() { delete(g.nodes, n) })
	return nil
}

func (g *Graph) deleteNode(n string, undo *[]func()) error {
	if _, ok := g.nodes[n]; !ok {
		return ErrNotFound
	}
	var removed []Edge
	for e := range g.edges {
		if e.From == n || e.To == n {
			delete(g.edges, e)
			delete(g.adj[e.From], e.To)
			removed = append(removed, e)
		}
	}
	delete(g.nodes, n)
	*undo = append(*undo, func() {
		g.nodes[n] = struct{}{}
		for _, e := range removed {
			g.edges[e] = struct{}{}
			if g.adj[e.From] == nil {
				g.adj[e.From] = make(map[string]struct{})
			}
			g.adj[e.From][e.To] = struct{}{}
		}
	})
	return nil
}

func (g *Graph) addEdge(from, to string, undo *[]func()) error {
	if _, ok := g.nodes[from]; !ok {
		return ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return ErrNotFound
	}
	e := Edge{From: from, To: to}
	if _, ok := g.edges[e]; ok {
		return ErrExists
	}
	if g.reachableLocked(to, from) {
		return ErrCycle
	}
	g.edges[e] = struct{}{}
	if g.adj[from] == nil {
		g.adj[from] = make(map[string]struct{})
	}
	g.adj[from][to] = struct{}{}
	*undo = append(*undo, func() {
		delete(g.edges, e)
		delete(g.adj[from], to)
	})
	return nil
}

func (g *Graph) deleteEdge(from, to string, undo *[]func()) error {
	e := Edge{From: from, To: to}
	if _, ok := g.edges[e]; !ok {
		return ErrNotFound
	}
	delete(g.edges, e)
	delete(g.adj[from], to)
	*undo = append(*undo, func() {
		g.edges[e] = struct{}{}
		if g.adj[from] == nil {
			g.adj[from] = make(map[string]struct{})
		}
		g.adj[from][to] = struct{}{}
	})
	return nil
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
		for m := range g.adj[n] {
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
	return g.snapshotLocked()
}

func (g *Graph) snapshotLocked() Snapshot {
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
