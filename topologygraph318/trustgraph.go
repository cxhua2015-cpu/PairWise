package topologygraph318

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
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
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
		out:      make(map[string]map[string]struct{}),
		in:       make(map[string]map[string]struct{}),
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

// validate checks the batch structurally without touching state.
func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, g.maxName) {
				return ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.maxName) || !validName(op.To, g.maxName) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func (g *Graph) addEdge(e Edge) {
	g.edges[e] = struct{}{}
	if g.out[e.From] == nil {
		g.out[e.From] = make(map[string]struct{})
	}
	g.out[e.From][e.To] = struct{}{}
	if g.in[e.To] == nil {
		g.in[e.To] = make(map[string]struct{})
	}
	g.in[e.To][e.From] = struct{}{}
}

func (g *Graph) removeEdge(e Edge) {
	delete(g.edges, e)
	delete(g.out[e.From], e.To)
	if len(g.out[e.From]) == 0 {
		delete(g.out, e.From)
	}
	delete(g.in[e.To], e.From)
	if len(g.in[e.To]) == 0 {
		delete(g.in, e.To)
	}
}

// reaches reports whether dst is reachable from src following out-edges.
func (g *Graph) reaches(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range g.out[n] {
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

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.validate(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	return g.applyLocked(b)
}

// applyLocked executes ops, tracking an undo log; on failure it rolls back.
func (g *Graph) applyLocked(b Batch) (Result, error) {
	type delNode struct {
		node  string
		edges []Edge
	}
	var addedNodes []string
	var delNodes []delNode
	var addedEdges []Edge
	var delEdges []Edge
	rollback := func() {
		for i := len(addedEdges) - 1; i >= 0; i-- {
			g.removeEdge(addedEdges[i])
		}
		for i := len(delEdges) - 1; i >= 0; i-- {
			g.addEdge(delEdges[i])
		}
		for i := len(addedNodes) - 1; i >= 0; i-- {
			delete(g.nodes, addedNodes[i])
		}
		for i := len(delNodes) - 1; i >= 0; i-- {
			g.nodes[delNodes[i].node] = struct{}{}
			for _, e := range delNodes[i].edges {
				g.addEdge(e)
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
			addedNodes = append(addedNodes, op.From)
		case DeleteNode:
			if _, ok := g.nodes[op.From]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			dn := delNode{node: op.From}
			for to := range g.out[op.From] {
				e := Edge{op.From, to}
				g.removeEdge(e)
				dn.edges = append(dn.edges, e)
			}
			for from := range g.in[op.From] {
				e := Edge{from, op.From}
				g.removeEdge(e)
				dn.edges = append(dn.edges, e)
			}
			delete(g.nodes, op.From)
			delNodes = append(delNodes, dn)
		case AddEdge:
			e := Edge{op.From, op.To}
			if _, ok := g.nodes[op.From]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			if _, ok := g.nodes[op.To]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			if _, ok := g.edges[e]; ok {
				rollback()
				return Result{}, ErrExists
			}
			if g.reaches(op.To, op.From) {
				rollback()
				return Result{}, ErrCycle
			}
			g.addEdge(e)
			addedEdges = append(addedEdges, e)
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := g.edges[e]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			g.removeEdge(e)
			delEdges = append(delEdges, e)
		}
	}
	if len(g.nodes) > g.maxNodes || len(g.edges) > g.maxEdges {
		rollback()
		return Result{}, ErrCapacity
	}
	g.generation++
	return Result{Generation: g.generation}, nil
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
	return g.reaches(from, to), nil
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
