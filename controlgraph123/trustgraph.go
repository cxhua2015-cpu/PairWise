package controlgraph123

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

func validName(s string, maxBytes int) bool {
	if s == "" || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// state is the mutable graph content. Apply works on a candidate copy and
// swaps it in only on success, so failures leave the graph untouched.
type state struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
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
	for n, set := range s.out {
		ns := make(map[string]struct{}, len(set))
		for m := range set {
			ns[m] = struct{}{}
		}
		c.out[n] = ns
	}
	for n, set := range s.in {
		ns := make(map[string]struct{}, len(set))
		for m := range set {
			ns[m] = struct{}{}
		}
		c.in[n] = ns
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
	if set := s.out[e.From]; set != nil {
		delete(set, e.To)
		if len(set) == 0 {
			delete(s.out, e.From)
		}
	}
	if set := s.in[e.To]; set != nil {
		delete(set, e.From)
		if len(set) == 0 {
			delete(s.in, e.To)
		}
	}
}

// reachable reports whether dst is reachable from src via directed edges.
func (s *state) reachable(src, dst string) bool {
	if src == dst {
		return true
	}
	visited := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range s.out[n] {
			if m == dst {
				return true
			}
			if _, ok := visited[m]; !ok {
				visited[m] = struct{}{}
				stack = append(stack, m)
			}
		}
	}
	return false
}

type Graph struct {
	mu         sync.RWMutex
	maxNodes   int
	maxEdges   int
	maxNameLen int
	st         *state
	generation uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxNodes:   o.MaxNodes,
		maxEdges:   o.MaxEdges,
		maxNameLen: o.MaxNameBytes,
		st:         newState(),
	}, nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching any state.
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, g.maxNameLen) {
				return Result{}, ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.maxNameLen) || !validName(op.To, g.maxNameLen) {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}

	// Phase 2: apply ops to a candidate copy.
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
			for to := range cand.out[op.From] {
				cand.removeEdge(Edge{From: op.From, To: to})
			}
			for from := range cand.in[op.From] {
				cand.removeEdge(Edge{From: from, To: op.From})
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
			cand.addEdge(e)
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := cand.edges[e]; !ok {
				return Result{}, ErrNotFound
			}
			cand.removeEdge(e)
		}
	}

	// Phase 3: capacity is only checked at the end of the batch.
	if len(cand.nodes) > g.maxNodes || len(cand.edges) > g.maxEdges {
		return Result{}, ErrCapacity
	}

	g.st = cand
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.maxNameLen) || !validName(to, g.maxNameLen) {
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
	nodes := make([]string, 0, len(g.st.nodes))
	for n := range g.st.nodes {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	edges := make([]Edge, 0, len(g.st.edges))
	for e := range g.st.edges {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return Snapshot{Generation: g.generation, Nodes: nodes, Edges: edges}
}
