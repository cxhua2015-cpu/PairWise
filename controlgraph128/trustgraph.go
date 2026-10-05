package controlgraph128

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
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
	edges int
}

func (s *state) clone() *state {
	c := &state{
		nodes: make(map[string]struct{}, len(s.nodes)),
		out:   make(map[string]map[string]struct{}, len(s.out)),
		in:    make(map[string]map[string]struct{}, len(s.in)),
		edges: s.edges,
	}
	for n := range s.nodes {
		c.nodes[n] = struct{}{}
	}
	for from, tos := range s.out {
		m := make(map[string]struct{}, len(tos))
		for to := range tos {
			m[to] = struct{}{}
		}
		c.out[from] = m
	}
	for to, froms := range s.in {
		m := make(map[string]struct{}, len(froms))
		for from := range froms {
			m[from] = struct{}{}
		}
		c.in[to] = m
	}
	return c
}

func (s *state) addEdge(from, to string) {
	if s.out[from] == nil {
		s.out[from] = make(map[string]struct{})
	}
	s.out[from][to] = struct{}{}
	if s.in[to] == nil {
		s.in[to] = make(map[string]struct{})
	}
	s.in[to][from] = struct{}{}
	s.edges++
}

func (s *state) removeEdge(from, to string) {
	delete(s.out[from], to)
	if len(s.out[from]) == 0 {
		delete(s.out, from)
	}
	delete(s.in[to], from)
	if len(s.in[to]) == 0 {
		delete(s.in, to)
	}
	s.edges--
}

func (s *state) removeNode(n string) {
	for to := range s.out[n] {
		s.removeEdge(n, to)
	}
	for from := range s.in[n] {
		s.removeEdge(from, n)
	}
	delete(s.nodes, n)
}

// reachable reports whether dst is reachable from src following out-edges.
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

type Graph struct {
	mu         sync.RWMutex
	opts       Options
	st         *state
	generation uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts: o,
		st: &state{
			nodes: make(map[string]struct{}),
			out:   make(map[string]map[string]struct{}),
			in:    make(map[string]map[string]struct{}),
		},
	}, nil
}

func validName(n string, maxBytes int) bool {
	if n == "" || len(n) > maxBytes {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
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

func (g *Graph) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		gen := g.generation
		g.mu.RUnlock()
		return Result{Generation: gen}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

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
			cand.removeNode(op.From)
		case AddEdge:
			if _, ok := cand.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.out[op.From][op.To]; ok {
				return Result{}, ErrExists
			}
			if cand.reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			cand.addEdge(op.From, op.To)
		case DeleteEdge:
			if _, ok := cand.out[op.From][op.To]; !ok {
				return Result{}, ErrNotFound
			}
			cand.removeEdge(op.From, op.To)
		}
	}
	if len(cand.nodes) > g.opts.MaxNodes || cand.edges > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.st = cand
	g.generation++
	return Result{Generation: g.generation}, nil
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
		Generation: g.generation,
		Nodes:      make([]string, 0, len(g.st.nodes)),
		Edges:      make([]Edge, 0, g.st.edges),
	}
	for n := range g.st.nodes {
		snap.Nodes = append(snap.Nodes, n)
	}
	sort.Strings(snap.Nodes)
	for from, tos := range g.st.out {
		for to := range tos {
			snap.Edges = append(snap.Edges, Edge{From: from, To: to})
		}
	}
	sort.Slice(snap.Edges, func(i, j int) bool {
		if snap.Edges[i].From != snap.Edges[j].From {
			return snap.Edges[i].From < snap.Edges[j].From
		}
		return snap.Edges[i].To < snap.Edges[j].To
	})
	return snap
}
