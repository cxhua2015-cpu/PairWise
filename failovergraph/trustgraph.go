package failovergraph

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

type state struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
}

func (s *state) clone() *state {
	c := &state{
		nodes: make(map[string]struct{}, len(s.nodes)),
		edges: make(map[Edge]struct{}, len(s.edges)),
	}
	for n := range s.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range s.edges {
		c.edges[e] = struct{}{}
	}
	return c
}

// reachable reports whether to is reachable from from within s.
func (s *state) reachable(from, to string) bool {
	if from == to {
		_, ok := s.nodes[from]
		return ok
	}
	seen := map[string]struct{}{from: {}}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for e := range s.edges {
			if e.From != cur {
				continue
			}
			if e.To == to {
				return true
			}
			if _, ok := seen[e.To]; !ok {
				seen[e.To] = struct{}{}
				queue = append(queue, e.To)
			}
		}
	}
	return false
}

type Graph struct {
	mu   sync.RWMutex
	opts Options
	st   *state
	gen  uint64
}

func validName(n string, maxBytes int) bool {
	if n == "" || len(n) > maxBytes {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{opts: o, st: &state{nodes: map[string]struct{}{}, edges: map[Edge]struct{}{}}}, nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	// Full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, g.opts.MaxNameBytes) {
				return Result{}, ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.opts.MaxNameBytes) || !validName(op.To, g.opts.MaxNameBytes) {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: g.gen}, nil
	}

	cand := g.st.clone()
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := cand.nodes[op.From]; ok {
				return Result{}, ErrExists
			}
			cand.nodes[op.From] = struct{}{}
		case DeleteNode:
			if _, ok := cand.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand.nodes, op.From)
			for e := range cand.edges {
				if e.From == op.From || e.To == op.From {
					delete(cand.edges, e)
				}
			}
		case AddEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := cand.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.edges[e]; ok {
				return Result{}, ErrExists
			}
			if op.From == op.To || cand.reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			cand.edges[e] = struct{}{}
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := cand.edges[e]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand.edges, e)
		}
	}

	if len(cand.nodes) > g.opts.MaxNodes || len(cand.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}

	g.st = cand
	g.gen++
	return Result{Generation: g.gen}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.opts.MaxNameBytes) || !validName(to, g.opts.MaxNameBytes) {
		return false, ErrInvalidInput
	}
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
	snap := Snapshot{
		Generation: g.gen,
		Nodes:      make([]string, 0, len(g.st.nodes)),
		Edges:      make([]Edge, 0, len(g.st.edges)),
	}
	for n := range g.st.nodes {
		snap.Nodes = append(snap.Nodes, n)
	}
	for e := range g.st.edges {
		snap.Edges = append(snap.Edges, e)
	}
	sort.Strings(snap.Nodes)
	sort.Slice(snap.Edges, func(i, j int) bool {
		if snap.Edges[i].From != snap.Edges[j].From {
			return snap.Edges[i].From < snap.Edges[j].From
		}
		return snap.Edges[i].To < snap.Edges[j].To
	})
	return snap
}
