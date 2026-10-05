package workflowgraph

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
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
	generation uint64
}

func New(opts Options) (*Graph, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  opts,
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		out:   make(map[string]map[string]struct{}),
		in:    make(map[string]map[string]struct{}),
	}, nil
}

func validName(s string, maxBytes int) bool {
	if s == "" || len(s) > maxBytes {
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

func (g *Graph) validateOp(op Op) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" || !validName(op.From, g.opts.MaxNameBytes) {
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

func (g *Graph) addNode(n string) error {
	if _, ok := g.nodes[n]; ok {
		return ErrExists
	}
	g.nodes[n] = struct{}{}
	return nil
}

func (g *Graph) deleteNode(n string) error {
	if _, ok := g.nodes[n]; !ok {
		return ErrNotFound
	}
	for to := range g.out[n] {
		delete(g.edges, Edge{n, to})
		delete(g.in[to], n)
	}
	delete(g.out, n)
	for from := range g.in[n] {
		delete(g.edges, Edge{from, n})
		delete(g.out[from], n)
	}
	delete(g.in, n)
	delete(g.nodes, n)
	return nil
}

func (g *Graph) reachableLocked(from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range g.out[cur] {
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
	delete(g.in[e.To], e.From)
}

func (g *Graph) addEdge(e Edge) error {
	if _, ok := g.nodes[e.From]; !ok {
		return ErrNotFound
	}
	if _, ok := g.nodes[e.To]; !ok {
		return ErrNotFound
	}
	if _, ok := g.edges[e]; ok {
		return ErrExists
	}
	if g.reachableLocked(e.To, e.From) {
		return ErrCycle
	}
	g.insertEdge(e)
	return nil
}

func (g *Graph) deleteEdge(e Edge) error {
	if _, ok := g.edges[e]; !ok {
		return ErrNotFound
	}
	g.removeEdge(e)
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}

	type undo struct {
		kind Kind
		node string
		edge Edge
	}
	var journal []undo
	rollback := func() {
		for i := len(journal) - 1; i >= 0; i-- {
			u := journal[i]
			switch u.kind {
			case AddNode:
				delete(g.nodes, u.node)
			case DeleteNode:
				g.nodes[u.node] = struct{}{}
			case AddEdge:
				g.removeEdge(u.edge)
			case DeleteEdge:
				g.insertEdge(u.edge)
			}
		}
	}

	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			if err = g.addNode(op.From); err == nil {
				journal = append(journal, undo{kind: AddNode, node: op.From})
			}
		case DeleteNode:
			var removed []Edge
			for to := range g.out[op.From] {
				removed = append(removed, Edge{op.From, to})
			}
			for from := range g.in[op.From] {
				removed = append(removed, Edge{from, op.From})
			}
			if err = g.deleteNode(op.From); err == nil {
				journal = append(journal, undo{kind: DeleteNode, node: op.From})
				for _, e := range removed {
					journal = append(journal, undo{kind: DeleteEdge, edge: e})
				}
			}
		case AddEdge:
			e := Edge{op.From, op.To}
			if err = g.addEdge(e); err == nil {
				journal = append(journal, undo{kind: AddEdge, edge: e})
			}
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if err = g.deleteEdge(e); err == nil {
				journal = append(journal, undo{kind: DeleteEdge, edge: e})
			}
		}
		if err != nil {
			rollback()
			return Result{}, err
		}
	}

	if len(g.nodes) > g.opts.MaxNodes || len(g.edges) > g.opts.MaxEdges {
		rollback()
		return Result{}, ErrCapacity
	}

	g.generation++
	return Result{Generation: g.generation}, nil
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
