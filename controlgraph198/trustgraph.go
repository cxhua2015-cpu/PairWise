package controlgraph198

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
	mu    sync.RWMutex
	opts  Options
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
	gen   uint64
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
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

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  o,
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		out:   make(map[string]map[string]struct{}),
		in:    make(map[string]map[string]struct{}),
	}, nil
}

// validate checks only the structural shape of ops, without reading state.
func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
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
	}
	return nil
}

func (g *Graph) addEdge(from, to string) {
	g.edges[Edge{from, to}] = struct{}{}
	if g.out[from] == nil {
		g.out[from] = make(map[string]struct{})
	}
	g.out[from][to] = struct{}{}
	if g.in[to] == nil {
		g.in[to] = make(map[string]struct{})
	}
	g.in[to][from] = struct{}{}
}

func (g *Graph) removeEdge(from, to string) {
	delete(g.edges, Edge{from, to})
	delete(g.out[from], to)
	delete(g.in[to], from)
}

// reaches reports whether target is reachable from start in the current
// (possibly candidate) state. Callers must hold the lock.
func (g *Graph) reaches(start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	stack := []string{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range g.out[n] {
			if m == target {
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
		return Result{Generation: g.gen}, nil
	}
	// Candidate transaction: record inverse ops for rollback.
	type undo struct {
		kind     Kind
		from, to string
	}
	var undos []undo
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			switch u.kind {
			case DeleteNode:
				delete(g.nodes, u.from)
			case AddNode:
				g.nodes[u.from] = struct{}{}
			case DeleteEdge:
				g.removeEdge(u.from, u.to)
			case AddEdge:
				g.addEdge(u.from, u.to)
			}
		}
	}
	fail := func(err error) (Result, error) {
		rollback()
		return Result{}, err
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := g.nodes[op.From]; ok {
				return fail(ErrExists)
			}
			g.nodes[op.From] = struct{}{}
			undos = append(undos, undo{DeleteNode, op.From, ""})
		case DeleteNode:
			if _, ok := g.nodes[op.From]; !ok {
				return fail(ErrNotFound)
			}
			var removed []Edge
			for to := range g.out[op.From] {
				removed = append(removed, Edge{op.From, to})
			}
			for from := range g.in[op.From] {
				removed = append(removed, Edge{from, op.From})
			}
			for _, e := range removed {
				g.removeEdge(e.From, e.To)
			}
			delete(g.nodes, op.From)
			undos = append(undos, undo{AddNode, op.From, ""})
			for _, e := range removed {
				undos = append(undos, undo{AddEdge, e.From, e.To})
			}
		case AddEdge:
			if _, ok := g.nodes[op.From]; !ok {
				return fail(ErrNotFound)
			}
			if _, ok := g.nodes[op.To]; !ok {
				return fail(ErrNotFound)
			}
			if _, ok := g.edges[Edge{op.From, op.To}]; ok {
				return fail(ErrExists)
			}
			if g.reaches(op.To, op.From) {
				return fail(ErrCycle)
			}
			g.addEdge(op.From, op.To)
			undos = append(undos, undo{DeleteEdge, op.From, op.To})
		case DeleteEdge:
			if _, ok := g.edges[Edge{op.From, op.To}]; !ok {
				return fail(ErrNotFound)
			}
			g.removeEdge(op.From, op.To)
			undos = append(undos, undo{AddEdge, op.From, op.To})
		}
	}
	if len(g.nodes) > g.opts.MaxNodes || len(g.edges) > g.opts.MaxEdges {
		rollback()
		return Result{}, ErrCapacity
	}
	g.gen++
	return Result{Generation: g.gen}, nil
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
	return g.reaches(from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := Snapshot{
		Generation: g.gen,
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
