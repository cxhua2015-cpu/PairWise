package subscriptiongraph

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

func (g *Graph) Apply(b Batch) (Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// 结构校验：完整校验后再读取状态。
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
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	var undo []func()
	rollback := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	for _, op := range b.Ops {
		var err error
		var undoFn func()
		switch op.Kind {
		case AddNode:
			undoFn, err = g.addNode(op.From)
		case DeleteNode:
			undoFn, err = g.deleteNode(op.From)
		case AddEdge:
			undoFn, err = g.addEdge(op.From, op.To)
		case DeleteEdge:
			undoFn, err = g.deleteEdge(op.From, op.To)
		}
		if err != nil {
			rollback()
			return Result{}, err
		}
		undo = append(undo, undoFn)
	}
	if len(g.nodes) > g.maxNodes || len(g.edges) > g.maxEdges {
		rollback()
		return Result{}, ErrCapacity
	}
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) addNode(n string) (func(), error) {
	if _, ok := g.nodes[n]; ok {
		return nil, ErrExists
	}
	g.nodes[n] = struct{}{}
	return func() { delete(g.nodes, n) }, nil
}

func (g *Graph) deleteNode(n string) (func(), error) {
	if _, ok := g.nodes[n]; !ok {
		return nil, ErrNotFound
	}
	delete(g.nodes, n)
	var removed []Edge
	for to := range g.out[n] {
		removed = append(removed, Edge{n, to})
	}
	for from := range g.in[n] {
		removed = append(removed, Edge{from, n})
	}
	for _, e := range removed {
		g.removeEdge(e)
	}
	return func() {
		g.nodes[n] = struct{}{}
		for _, e := range removed {
			g.insertEdge(e)
		}
	}, nil
}

func (g *Graph) insertEdge(e Edge) {
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

func (g *Graph) addEdge(from, to string) (func(), error) {
	if _, ok := g.nodes[from]; !ok {
		return nil, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return nil, ErrNotFound
	}
	e := Edge{from, to}
	if _, ok := g.edges[e]; ok {
		return nil, ErrExists
	}
	if from == to || g.reachableLocked(to, from) {
		return nil, ErrCycle
	}
	g.insertEdge(e)
	return func() { g.removeEdge(e) }, nil
}

func (g *Graph) deleteEdge(from, to string) (func(), error) {
	e := Edge{from, to}
	if _, ok := g.edges[e]; !ok {
		return nil, ErrNotFound
	}
	g.removeEdge(e)
	return func() { g.insertEdge(e) }, nil
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
	g.mu.RLock()
	defer g.mu.RUnlock()
	if !validName(from, g.maxName) || !validName(to, g.maxName) {
		return false, ErrInvalidInput
	}
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
