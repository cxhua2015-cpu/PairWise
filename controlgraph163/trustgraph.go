package controlgraph163

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

type edgeKey struct{ from, to string }

type state struct {
	nodes map[string]struct{}
	edges map[edgeKey]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

func newState() *state {
	return &state{
		nodes: make(map[string]struct{}),
		edges: make(map[edgeKey]struct{}),
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
	for k, m := range s.out {
		cm := make(map[string]struct{}, len(m))
		for v := range m {
			cm[v] = struct{}{}
		}
		c.out[k] = cm
	}
	for k, m := range s.in {
		cm := make(map[string]struct{}, len(m))
		for v := range m {
			cm[v] = struct{}{}
		}
		c.in[k] = cm
	}
	return c
}

func (s *state) addEdge(from, to string) {
	s.edges[edgeKey{from, to}] = struct{}{}
	if s.out[from] == nil {
		s.out[from] = make(map[string]struct{})
	}
	s.out[from][to] = struct{}{}
	if s.in[to] == nil {
		s.in[to] = make(map[string]struct{})
	}
	s.in[to][from] = struct{}{}
}

func (s *state) removeEdge(from, to string) {
	delete(s.edges, edgeKey{from, to})
	if m := s.out[from]; m != nil {
		delete(m, to)
		if len(m) == 0 {
			delete(s.out, from)
		}
	}
	if m := s.in[to]; m != nil {
		delete(m, from)
		if len(m) == 0 {
			delete(s.in, to)
		}
	}
}

func (s *state) removeNode(n string) {
	delete(s.nodes, n)
	for to := range s.out[n] {
		s.removeEdge(n, to)
	}
	for from := range s.in[n] {
		s.removeEdge(from, n)
	}
}

// reachable reports whether dst is reachable from src following out-edges.
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

type Graph struct {
	mu         sync.RWMutex
	maxNodes   int
	maxEdges   int
	maxName    int
	st         *state
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
		st:       newState(),
	}, nil
}

func validName(n string, max int) bool {
	if n == "" || len(n) > max {
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

// validate checks the whole batch structurally before any state is read.
func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
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
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.validate(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
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
			cand.removeNode(op.From)
		case AddEdge:
			if _, ok := cand.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.edges[edgeKey{op.From, op.To}]; ok {
				return Result{}, ErrExists
			}
			if cand.reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			cand.addEdge(op.From, op.To)
		case DeleteEdge:
			if _, ok := cand.edges[edgeKey{op.From, op.To}]; !ok {
				return Result{}, ErrNotFound
			}
			cand.removeEdge(op.From, op.To)
		}
	}
	if len(cand.nodes) > g.maxNodes || len(cand.edges) > g.maxEdges {
		return Result{}, ErrCapacity
	}
	g.st = cand
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.maxName) || !validName(to, g.maxName) {
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
		Edges:      make([]Edge, 0, len(g.st.edges)),
	}
	for n := range g.st.nodes {
		snap.Nodes = append(snap.Nodes, n)
	}
	sort.Strings(snap.Nodes)
	for e := range g.st.edges {
		snap.Edges = append(snap.Edges, Edge{From: e.from, To: e.to})
	}
	sort.Slice(snap.Edges, func(i, j int) bool {
		if snap.Edges[i].From != snap.Edges[j].From {
			return snap.Edges[i].From < snap.Edges[j].From
		}
		return snap.Edges[i].To < snap.Edges[j].To
	})
	return snap
}
