package topologygraph423

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

// state is the mutable graph content guarded by Graph.mu.
type state struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
}

func newState() state {
	return state{nodes: make(map[string]struct{}), edges: make(map[Edge]struct{})}
}

// copy returns a fully independent deep copy of s.
func (s state) copy() state {
	c := state{nodes: make(map[string]struct{}, len(s.nodes)), edges: make(map[Edge]struct{}, len(s.edges))}
	for n := range s.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range s.edges {
		c.edges[e] = struct{}{}
	}
	return c
}

// reachable reports whether to is reachable from from within s.
func (s state) reachable(from, to string) bool {
	if from == to {
		return true
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

// applyOp mutates s according to a single structurally valid op.
func (s state) applyOp(op Op) error {
	switch op.Kind {
	case AddNode:
		if _, ok := s.nodes[op.From]; ok {
			return ErrExists
		}
		s.nodes[op.From] = struct{}{}
	case DeleteNode:
		if _, ok := s.nodes[op.From]; !ok {
			return ErrNotFound
		}
		delete(s.nodes, op.From)
		for e := range s.edges {
			if e.From == op.From || e.To == op.From {
				delete(s.edges, e)
			}
		}
	case AddEdge:
		if _, ok := s.nodes[op.From]; !ok {
			return ErrNotFound
		}
		if _, ok := s.nodes[op.To]; !ok {
			return ErrNotFound
		}
		e := Edge{From: op.From, To: op.To}
		if _, ok := s.edges[e]; ok {
			return ErrExists
		}
		if s.reachable(op.To, op.From) {
			return ErrCycle
		}
		s.edges[e] = struct{}{}
	case DeleteEdge:
		e := Edge{From: op.From, To: op.To}
		if _, ok := s.edges[e]; !ok {
			return ErrNotFound
		}
		delete(s.edges, e)
	}
	return nil
}

// Graph is a concurrency-safe in-memory control topology graph.
type Graph struct {
	mu         sync.RWMutex
	st         state
	generation uint64
	opts       Options
}

func New(opts Options) (*Graph, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{st: newState(), opts: opts}, nil
}

// apply runs the shared transaction semantics on a candidate copy of st and
// returns the resulting candidate state. st is never mutated.
func (g *Graph) apply(st state, b Batch) (state, error) {
	if err := g.ValidateBatch(b); err != nil {
		return state{}, err
	}
	cand := st.copy()
	for _, op := range b.Ops {
		if err := cand.applyOp(op); err != nil {
			return state{}, err
		}
	}
	if len(cand.nodes) > g.opts.MaxNodes || len(cand.edges) > g.opts.MaxEdges {
		return state{}, ErrCapacity
	}
	return cand, nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cand, err := g.apply(g.st, b)
	if err != nil {
		return Result{}, err
	}
	g.st = cand
	if len(b.Ops) > 0 {
		g.generation++
	}
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !g.validName(from) || !g.validName(to) {
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
	return snapshotOf(g.st, g.generation)
}

// snapshotOf builds a stably sorted snapshot from an arbitrary state.
func snapshotOf(st state, generation uint64) Snapshot {
	snap := Snapshot{
		Generation: generation,
		Nodes:      make([]string, 0, len(st.nodes)),
		Edges:      make([]Edge, 0, len(st.edges)),
	}
	for n := range st.nodes {
		snap.Nodes = append(snap.Nodes, n)
	}
	for e := range st.edges {
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
