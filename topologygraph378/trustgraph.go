package topologygraph378

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
	mu      sync.RWMutex
	maxNode int
	maxEdge int
	maxName int
	nodes   map[string]struct{}
	edges   map[Edge]struct{}
	out     map[string]map[string]struct{}
	in      map[string]map[string]struct{}
	gen     uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxNode: o.MaxNodes,
		maxEdge: o.MaxEdges,
		maxName: o.MaxNameBytes,
		nodes:   map[string]struct{}{},
		edges:   map[Edge]struct{}{},
		out:     map[string]map[string]struct{}{},
		in:      map[string]map[string]struct{}{},
	}, nil
}

func validName(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validateOp performs structural validation only; it must not read state.
func (g *Graph) validateOp(op Op) error {
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
	return nil
}

func (g *Graph) addNode(n string) error {
	if _, ok := g.nodes[n]; ok {
		return ErrExists
	}
	g.nodes[n] = struct{}{}
	return nil
}

func (g *Graph) removeNode(n string) {
	delete(g.nodes, n)
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
	g.edges[e] = struct{}{}
	if g.out[e.From] == nil {
		g.out[e.From] = map[string]struct{}{}
	}
	g.out[e.From][e.To] = struct{}{}
	if g.in[e.To] == nil {
		g.in[e.To] = map[string]struct{}{}
	}
	g.in[e.To][e.From] = struct{}{}
	return nil
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

// deleteNode removes n and all incident edges, returning them for undo.
func (g *Graph) deleteNode(n string) ([]Edge, error) {
	if _, ok := g.nodes[n]; !ok {
		return nil, ErrNotFound
	}
	var removed []Edge
	for m := range g.out[n] {
		e := Edge{n, m}
		delete(g.edges, e)
		delete(g.in[m], n)
		if len(g.in[m]) == 0 {
			delete(g.in, m)
		}
		removed = append(removed, e)
	}
	delete(g.out, n)
	for m := range g.in[n] {
		e := Edge{m, n}
		delete(g.edges, e)
		delete(g.out[m], n)
		if len(g.out[m]) == 0 {
			delete(g.out, m)
		}
		removed = append(removed, e)
	}
	delete(g.in, n)
	delete(g.nodes, n)
	return removed, nil
}

func (g *Graph) reachableLocked(from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for next := range g.out[cur] {
			if next == to {
				return true
			}
			if _, ok := seen[next]; !ok {
				seen[next] = struct{}{}
				queue = append(queue, next)
			}
		}
	}
	return false
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
		return Result{Generation: g.gen}, nil
	}
	type undo struct {
		kind  Kind
		edge  Edge
		edges []Edge
	}
	var log []undo
	rollback := func() {
		for i := len(log) - 1; i >= 0; i-- {
			u := log[i]
			switch u.kind {
			case AddNode:
				g.removeNode(u.edge.From)
			case DeleteNode:
				g.nodes[u.edge.From] = struct{}{}
				for _, e := range u.edges {
					g.edges[e] = struct{}{}
					if g.out[e.From] == nil {
						g.out[e.From] = map[string]struct{}{}
					}
					g.out[e.From][e.To] = struct{}{}
					if g.in[e.To] == nil {
						g.in[e.To] = map[string]struct{}{}
					}
					g.in[e.To][e.From] = struct{}{}
				}
			case AddEdge:
				g.removeEdge(u.edge)
			case DeleteEdge:
				g.edges[u.edge] = struct{}{}
				if g.out[u.edge.From] == nil {
					g.out[u.edge.From] = map[string]struct{}{}
				}
				g.out[u.edge.From][u.edge.To] = struct{}{}
				if g.in[u.edge.To] == nil {
					g.in[u.edge.To] = map[string]struct{}{}
				}
				g.in[u.edge.To][u.edge.From] = struct{}{}
			}
		}
	}
	for _, op := range b.Ops {
		var u undo
		var err error
		switch op.Kind {
		case AddNode:
			err = g.addNode(op.From)
			u = undo{kind: AddNode, edge: Edge{From: op.From}}
		case DeleteNode:
			var removed []Edge
			removed, err = g.deleteNode(op.From)
			u = undo{kind: DeleteNode, edge: Edge{From: op.From}, edges: removed}
		case AddEdge:
			err = g.addEdge(Edge{op.From, op.To})
			u = undo{kind: AddEdge, edge: Edge{op.From, op.To}}
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := g.edges[e]; !ok {
				err = ErrNotFound
			} else {
				g.removeEdge(e)
			}
			u = undo{kind: DeleteEdge, edge: e}
		}
		if err != nil {
			rollback()
			return Result{}, err
		}
		log = append(log, u)
	}
	if len(g.nodes) > g.maxNode || len(g.edges) > g.maxEdge {
		rollback()
		return Result{}, ErrCapacity
	}
	g.gen++
	return Result{Generation: g.gen}, nil
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
	s := Snapshot{Generation: g.gen, Nodes: make([]string, 0, len(g.nodes)), Edges: make([]Edge, 0, len(g.edges))}
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
