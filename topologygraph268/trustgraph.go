package topologygraph268

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

// state is the committed graph content. Graph swaps it atomically at the
// end of every successful non-empty batch.
type state struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
}

func newState() *state {
	return &state{
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		out:   make(map[string]map[string]struct{}),
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
	for from, tos := range s.out {
		m := make(map[string]struct{}, len(tos))
		for to := range tos {
			m[to] = struct{}{}
		}
		c.out[from] = m
	}
	return c
}

func (s *state) addNode(name string) {
	s.nodes[name] = struct{}{}
}

func (s *state) deleteNode(name string) {
	delete(s.nodes, name)
	for e := range s.edges {
		if e.From == name || e.To == name {
			delete(s.edges, e)
		}
	}
	for from, tos := range s.out {
		if from == name {
			delete(s.out, from)
			continue
		}
		delete(tos, name)
		if len(tos) == 0 {
			delete(s.out, from)
		}
	}
}

func (s *state) addEdge(e Edge) {
	s.edges[e] = struct{}{}
	tos, ok := s.out[e.From]
	if !ok {
		tos = make(map[string]struct{})
		s.out[e.From] = tos
	}
	tos[e.To] = struct{}{}
}

func (s *state) deleteEdge(e Edge) {
	delete(s.edges, e)
	if tos, ok := s.out[e.From]; ok {
		delete(tos, e.To)
		if len(tos) == 0 {
			delete(s.out, e.From)
		}
	}
}

// reachable reports whether dst is reachable from src following directed
// edges. src == dst is reachable.
func (s *state) reachable(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	queue := []string{src}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for next := range s.out[cur] {
			if next == dst {
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

// Graph is a concurrency-safe in-memory control topology graph.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	st         *state
	generation uint64
}

func New(opts Options) (*Graph, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{opts: opts, st: newState()}, nil
}

// Apply validates the batch structurally, replays it against a candidate
// copy of the committed state, checks final capacity, and commits
// atomically. Any failure rolls the whole batch back.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	cand := g.st.clone()
	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = applyAddNode(cand, op.From)
		case DeleteNode:
			err = applyDeleteNode(cand, op.From)
		case AddEdge:
			err = applyAddEdge(cand, Edge{From: op.From, To: op.To})
		case DeleteEdge:
			err = applyDeleteEdge(cand, Edge{From: op.From, To: op.To})
		}
		if err != nil {
			return Result{}, err
		}
	}
	if len(cand.nodes) > g.opts.MaxNodes || len(cand.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.st = cand
	g.generation++
	return Result{Generation: g.generation}, nil
}

func applyAddNode(s *state, name string) error {
	if _, ok := s.nodes[name]; ok {
		return ErrExists
	}
	s.addNode(name)
	return nil
}

func applyDeleteNode(s *state, name string) error {
	if _, ok := s.nodes[name]; !ok {
		return ErrNotFound
	}
	s.deleteNode(name)
	return nil
}

func applyAddEdge(s *state, e Edge) error {
	if _, ok := s.nodes[e.From]; !ok {
		return ErrNotFound
	}
	if _, ok := s.nodes[e.To]; !ok {
		return ErrNotFound
	}
	if _, ok := s.edges[e]; ok {
		return ErrExists
	}
	if s.reachable(e.To, e.From) {
		return ErrCycle
	}
	s.addEdge(e)
	return nil
}

func applyDeleteEdge(s *state, e Edge) error {
	if _, ok := s.edges[e]; !ok {
		return ErrNotFound
	}
	s.deleteEdge(e)
	return nil
}

// Reachable reports whether dst is reachable from src in the current
// consistent snapshot of the graph.
func (g *Graph) Reachable(src, dst string) (bool, error) {
	if err := validateName(src, g.opts.MaxNameBytes); err != nil {
		return false, err
	}
	if err := validateName(dst, g.opts.MaxNameBytes); err != nil {
		return false, err
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.st.nodes[src]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.st.nodes[dst]; !ok {
		return false, ErrNotFound
	}
	return g.st.reachable(src, dst), nil
}

// Snapshot returns a stably sorted, fully detached view of the graph.
func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	snap := Snapshot{
		Generation: g.generation,
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
