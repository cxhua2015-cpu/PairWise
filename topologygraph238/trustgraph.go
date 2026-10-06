package topologygraph238

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

// Graph is a concurrency-safe in-memory control topology graph.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
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
		opts:  o,
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		out:   make(map[string]map[string]struct{}),
	}, nil
}

// txn is a candidate transaction: it stages mutations on copies so a
// failed batch leaves the committed state untouched.
type txn struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
}

func (g *Graph) newTxn() *txn {
	t := &txn{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
	}
	for n := range g.nodes {
		t.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		t.edges[e] = struct{}{}
	}
	for from, tos := range g.out {
		s := make(map[string]struct{}, len(tos))
		for to := range tos {
			s[to] = struct{}{}
		}
		t.out[from] = s
	}
	return t
}

func (t *txn) addNode(n string) error {
	if _, ok := t.nodes[n]; ok {
		return ErrExists
	}
	t.nodes[n] = struct{}{}
	return nil
}

func (t *txn) deleteNode(n string) error {
	if _, ok := t.nodes[n]; !ok {
		return ErrNotFound
	}
	delete(t.nodes, n)
	for to := range t.out[n] {
		delete(t.edges, Edge{n, to})
	}
	delete(t.out, n)
	for from, tos := range t.out {
		if _, ok := tos[n]; ok {
			delete(tos, n)
			delete(t.edges, Edge{from, n})
		}
	}
	return nil
}

func (t *txn) addEdge(from, to string) error {
	if _, ok := t.nodes[from]; !ok {
		return ErrNotFound
	}
	if _, ok := t.nodes[to]; !ok {
		return ErrNotFound
	}
	e := Edge{from, to}
	if _, ok := t.edges[e]; ok {
		return ErrExists
	}
	if t.reachable(to, from) {
		return ErrCycle
	}
	t.edges[e] = struct{}{}
	if t.out[from] == nil {
		t.out[from] = make(map[string]struct{})
	}
	t.out[from][to] = struct{}{}
	return nil
}

func (t *txn) deleteEdge(from, to string) error {
	e := Edge{from, to}
	if _, ok := t.edges[e]; !ok {
		return ErrNotFound
	}
	delete(t.edges, e)
	delete(t.out[from], to)
	return nil
}

// reachable reports whether target is reachable from start via directed edges.
func (t *txn) reachable(start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	stack := []string{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for to := range t.out[n] {
			if to == target {
				return true
			}
			if _, ok := seen[to]; !ok {
				seen[to] = struct{}{}
				stack = append(stack, to)
			}
		}
	}
	return false
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	t := g.newTxn()
	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = t.addNode(op.From)
		case DeleteNode:
			err = t.deleteNode(op.From)
		case AddEdge:
			err = t.addEdge(op.From, op.To)
		case DeleteEdge:
			err = t.deleteEdge(op.From, op.To)
		}
		if err != nil {
			return Result{}, err
		}
	}
	if len(t.nodes) > g.opts.MaxNodes || len(t.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges, g.out = t.nodes, t.edges, t.out
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
	t := &txn{out: g.out}
	return t.reachable(from, to), nil
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
