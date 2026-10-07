package topologygraph413

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

// state is the mutable graph payload. Apply builds a candidate copy of
// state, mutates it, and atomically swaps it in only on success, so a
// failed batch leaves the committed state untouched.
type state struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{} // adjacency index: from -> to set
	in    map[string]map[string]struct{} // reverse index: to -> from set
}

func newState() *state {
	return &state{
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		out:   make(map[string]map[string]struct{}),
		in:    make(map[string]map[string]struct{}),
	}
}

func (s *state) clone() *state {
	c := newState()
	for n := range s.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range s.edges {
		c.edges[e] = struct{}{}
	}
	for f, tos := range s.out {
		m := make(map[string]struct{}, len(tos))
		for t := range tos {
			m[t] = struct{}{}
		}
		c.out[f] = m
	}
	for t, froms := range s.in {
		m := make(map[string]struct{}, len(froms))
		for f := range froms {
			m[f] = struct{}{}
		}
		c.in[t] = m
	}
	return c
}

func (s *state) addEdge(e Edge) {
	s.edges[e] = struct{}{}
	if s.out[e.From] == nil {
		s.out[e.From] = make(map[string]struct{})
	}
	s.out[e.From][e.To] = struct{}{}
	if s.in[e.To] == nil {
		s.in[e.To] = make(map[string]struct{})
	}
	s.in[e.To][e.From] = struct{}{}
}

func (s *state) removeEdge(e Edge) {
	delete(s.edges, e)
	if tos := s.out[e.From]; tos != nil {
		delete(tos, e.To)
		if len(tos) == 0 {
			delete(s.out, e.From)
		}
	}
	if froms := s.in[e.To]; froms != nil {
		delete(froms, e.From)
		if len(froms) == 0 {
			delete(s.in, e.To)
		}
	}
}

func (s *state) removeNode(n string) {
	delete(s.nodes, n)
	for t := range s.out[n] {
		s.removeEdge(Edge{From: n, To: t})
	}
	for f := range s.in[n] {
		s.removeEdge(Edge{From: f, To: n})
	}
}

// reachable reports whether dst is reachable from src following directed
// edges. Callers must hold at least a read lock, or use a private state.
func (s *state) reachable(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range s.out[cur] {
			if next == dst {
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

// Graph is a concurrency-safe in-memory control topology graph.
// All public methods may be called concurrently.
type Graph struct {
	mu   sync.RWMutex
	opts Options
	st   *state
	gen  uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{opts: o, st: newState()}, nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}
	cand := g.st.clone()
	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			if _, ok := cand.nodes[op.From]; ok {
				err = ErrExists
			} else {
				cand.nodes[op.From] = struct{}{}
			}
		case DeleteNode:
			if _, ok := cand.nodes[op.From]; !ok {
				err = ErrNotFound
			} else {
				cand.removeNode(op.From)
			}
		case AddEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := cand.nodes[op.From]; !ok {
				err = ErrNotFound
			} else if _, ok := cand.nodes[op.To]; !ok {
				err = ErrNotFound
			} else if _, ok := cand.edges[e]; ok {
				err = ErrExists
			} else if cand.reachable(op.To, op.From) {
				err = ErrCycle
			} else {
				cand.addEdge(e)
			}
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := cand.edges[e]; !ok {
				err = ErrNotFound
			} else {
				cand.removeEdge(e)
			}
		}
		if err != nil {
			return Result{}, err // candidate discarded: full rollback
		}
	}
	// Capacity is checked only against the final state of the batch.
	if len(cand.nodes) > g.opts.MaxNodes || len(cand.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.st = cand
	g.gen++
	return Result{Generation: g.gen}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.st.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.st.nodes[to]; !ok {
		return false, ErrNotFound
	}
	return g.st.reachable(from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.snapshotLocked()
}

func (g *Graph) snapshotLocked() Snapshot {
	snap := Snapshot{
		Generation: g.gen,
		Nodes:      make([]string, 0, len(g.st.nodes)),
		Edges:      make([]Edge, 0, len(g.st.edges)),
	}
	for n := range g.st.nodes {
		snap.Nodes = append(snap.Nodes, n)
	}
	sort.Strings(snap.Nodes)
	for e := range g.st.edges {
		snap.Edges = append(snap.Edges, e)
	}
	sort.Slice(snap.Edges, func(i, j int) bool {
		if snap.Edges[i].From != snap.Edges[j].From {
			return snap.Edges[i].From < snap.Edges[j].From
		}
		return snap.Edges[i].To < snap.Edges[j].To
	})
	return snap
}
