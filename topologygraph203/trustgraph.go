package topologygraph203

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
	out        map[string]map[string]struct{}
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
		out:        make(map[string]map[string]struct{}),
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
			if !validName(op.From, g.maxNameLen) {
				return Result{}, ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.maxNameLen) || !validName(op.To, g.maxNameLen) {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		gen := g.generation
		g.mu.RUnlock()
		return Result{Generation: gen}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	type undo struct {
		kind     Kind
		from, to string
	}
	var undos []undo
	rollback := func(err error) (Result, error) {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			switch u.kind {
			case AddNode:
				g.addNode(u.from)
			case DeleteNode:
				g.deleteNode(u.from)
			case AddEdge:
				g.addEdge(u.from, u.to)
			case DeleteEdge:
				g.deleteEdge(u.from, u.to)
			}
		}
		return Result{}, err
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := g.nodes[op.From]; ok {
				return rollback(ErrExists)
			}
			g.addNode(op.From)
			undos = append(undos, undo{DeleteNode, op.From, ""})
		case DeleteNode:
			if _, ok := g.nodes[op.From]; !ok {
				return rollback(ErrNotFound)
			}
			var removed []Edge
			for e := range g.edges {
				if e.From == op.From || e.To == op.From {
					removed = append(removed, e)
				}
			}
			for _, e := range removed {
				g.deleteEdge(e.From, e.To)
			}
			g.deleteNode(op.From)
			undos = append(undos, undo{AddNode, op.From, ""})
			for _, e := range removed {
				undos = append(undos, undo{AddEdge, e.From, e.To})
			}
		case AddEdge:
			if _, ok := g.nodes[op.From]; !ok {
				return rollback(ErrNotFound)
			}
			if _, ok := g.nodes[op.To]; !ok {
				return rollback(ErrNotFound)
			}
			e := Edge{op.From, op.To}
			if _, ok := g.edges[e]; ok {
				return rollback(ErrExists)
			}
			if g.reachableLocked(op.To, op.From) {
				return rollback(ErrCycle)
			}
			g.addEdge(op.From, op.To)
			undos = append(undos, undo{DeleteEdge, op.From, op.To})
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := g.edges[e]; !ok {
				return rollback(ErrNotFound)
			}
			g.deleteEdge(op.From, op.To)
			undos = append(undos, undo{AddEdge, op.From, op.To})
		}
	}

	if len(g.nodes) > g.maxNodes || len(g.edges) > g.maxEdges {
		return rollback(ErrCapacity)
	}
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) addNode(n string) {
	g.nodes[n] = struct{}{}
}

func (g *Graph) deleteNode(n string) {
	delete(g.nodes, n)
	delete(g.out, n)
}

func (g *Graph) addEdge(from, to string) {
	g.edges[Edge{from, to}] = struct{}{}
	if g.out[from] == nil {
		g.out[from] = make(map[string]struct{})
	}
	g.out[from][to] = struct{}{}
}

func (g *Graph) deleteEdge(from, to string) {
	delete(g.edges, Edge{from, to})
	if m := g.out[from]; m != nil {
		delete(m, to)
		if len(m) == 0 {
			delete(g.out, from)
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
		for next := range g.out[n] {
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
	return g.reachableLocked(from, to), nil
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
