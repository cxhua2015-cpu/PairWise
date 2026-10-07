package topologygraph313

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

// state is the immutable-once-swapped graph content. A batch clones it,
// mutates the clone (candidate transaction), and swaps it in on success.
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
	for k, m := range s.out {
		nm := make(map[string]struct{}, len(m))
		for v := range m {
			nm[v] = struct{}{}
		}
		c.out[k] = nm
	}
	for k, m := range s.in {
		nm := make(map[string]struct{}, len(m))
		for v := range m {
			nm[v] = struct{}{}
		}
		c.in[k] = nm
	}
	return c
}

func (s *state) addNode(n string) {
	s.nodes[n] = struct{}{}
}

func (s *state) delNode(n string) {
	delete(s.nodes, n)
	for to := range s.out[n] {
		delete(s.edges, Edge{n, to})
		delete(s.in[to], n)
	}
	delete(s.out, n)
	for from := range s.in[n] {
		delete(s.edges, Edge{from, n})
		delete(s.out[from], n)
	}
	delete(s.in, n)
}

func (s *state) addEdge(from, to string) {
	s.edges[Edge{from, to}] = struct{}{}
	if s.out[from] == nil {
		s.out[from] = make(map[string]struct{})
	}
	s.out[from][to] = struct{}{}
	if s.in[to] == nil {
		s.in[to] = make(map[string]struct{})
	}
	s.in[to][from] = struct{}{}
}

func (s *state) delEdge(from, to string) {
	delete(s.edges, Edge{from, to})
	delete(s.out[from], to)
	delete(s.in[to], from)
}

// reachable reports whether target is reachable from start, following
// directed edges. start == target counts as reachable.
func (s *state) reachable(start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for next := range s.out[cur] {
			if next == target {
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

func validName(s string, maxBytes int) bool {
	if s == "" || len(s) > maxBytes {
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
func validateOp(op Op, maxNameBytes int) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" {
			return ErrInvalidInput
		}
		if !validName(op.From, maxNameBytes) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(op.From, maxNameBytes) || !validName(op.To, maxNameBytes) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

type Graph struct {
	mu      sync.RWMutex
	st      *state
	gen     uint64
	maxNode int
	maxEdge int
	maxName int
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		st:      newState(),
		maxNode: o.MaxNodes,
		maxEdge: o.MaxEdges,
		maxName: o.MaxNameBytes,
	}, nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	// Full structural validation before touching any state.
	for _, op := range b.Ops {
		if err := validateOp(op, g.maxName); err != nil {
			return Result{}, err
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
			cand.addNode(op.From)
		case DeleteNode:
			if _, ok := cand.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			cand.delNode(op.From)
		case AddEdge:
			if _, ok := cand.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand.edges[Edge{op.From, op.To}]; ok {
				return Result{}, ErrExists
			}
			if cand.reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			cand.addEdge(op.From, op.To)
		case DeleteEdge:
			if _, ok := cand.edges[Edge{op.From, op.To}]; !ok {
				return Result{}, ErrNotFound
			}
			cand.delEdge(op.From, op.To)
		}
	}

	// Capacity is checked only against the final candidate state.
	if len(cand.nodes) > g.maxNode || len(cand.edges) > g.maxEdge {
		return Result{}, ErrCapacity
	}

	g.st = cand
	g.gen++
	return Result{Generation: g.gen}, nil
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
	return Snapshot{Generation: g.gen, Nodes: nodes, Edges: edges}
}
