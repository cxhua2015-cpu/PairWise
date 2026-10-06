package topologygraph208

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

type Graph struct {
	mu         sync.RWMutex
	maxNodes   int
	maxEdges   int
	maxName    int
	generation uint64
	nodes      map[string]struct{}
	edges      map[edgeKey]struct{}
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxNodes: o.MaxNodes,
		maxEdges: o.MaxEdges,
		maxName:  o.MaxNameBytes,
		nodes:    make(map[string]struct{}),
		edges:    make(map[edgeKey]struct{}),
		out:      make(map[string]map[string]struct{}),
		in:       make(map[string]map[string]struct{}),
	}, nil
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

func (g *Graph) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, g.maxName) {
				return Result{}, ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.maxName) || !validName(op.To, g.maxName) {
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

	// Phase 2: apply to a candidate transaction cloned from current state.
	c := g.candidate()
	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = c.addNode(op.From)
		case DeleteNode:
			err = c.deleteNode(op.From)
		case AddEdge:
			err = c.addEdge(op.From, op.To)
		case DeleteEdge:
			err = c.deleteEdge(op.From, op.To)
		}
		if err != nil {
			return Result{}, err
		}
	}
	if len(c.nodes) > g.maxNodes || len(c.edges) > g.maxEdges {
		return Result{}, ErrCapacity
	}

	// Commit.
	g.nodes, g.edges, g.out, g.in = c.nodes, c.edges, c.out, c.in
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.maxName) || !validName(to, g.maxName) {
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
	return reaches(g.out, from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	nodes := make([]string, 0, len(g.nodes))
	for n := range g.nodes {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	edges := make([]Edge, 0, len(g.edges))
	for e := range g.edges {
		edges = append(edges, Edge{From: e.from, To: e.to})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return Snapshot{Generation: g.generation, Nodes: nodes, Edges: edges}
}

// reaches reports whether `to` is reachable from `from` following out-edges.
// A node is reachable from itself.
func reaches(out map[string]map[string]struct{}, from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range out[n] {
			if m == to {
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

// candidate is a mutable transaction cloned from the graph's committed state.
type candidate struct {
	nodes map[string]struct{}
	edges map[edgeKey]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

func (g *Graph) candidate() *candidate {
	c := &candidate{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[edgeKey]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
		in:    make(map[string]map[string]struct{}, len(g.in)),
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for n, set := range g.out {
		s := make(map[string]struct{}, len(set))
		for m := range set {
			s[m] = struct{}{}
		}
		c.out[n] = s
	}
	for n, set := range g.in {
		s := make(map[string]struct{}, len(set))
		for m := range set {
			s[m] = struct{}{}
		}
		c.in[n] = s
	}
	return c
}

func (c *candidate) addNode(n string) error {
	if _, ok := c.nodes[n]; ok {
		return ErrExists
	}
	c.nodes[n] = struct{}{}
	return nil
}

func (c *candidate) deleteNode(n string) error {
	if _, ok := c.nodes[n]; !ok {
		return ErrNotFound
	}
	for m := range c.out[n] {
		delete(c.edges, edgeKey{n, m})
		delete(c.in[m], n)
	}
	for m := range c.in[n] {
		delete(c.edges, edgeKey{m, n})
		delete(c.out[m], n)
	}
	delete(c.nodes, n)
	delete(c.out, n)
	delete(c.in, n)
	return nil
}

func (c *candidate) addEdge(from, to string) error {
	if _, ok := c.nodes[from]; !ok {
		return ErrNotFound
	}
	if _, ok := c.nodes[to]; !ok {
		return ErrNotFound
	}
	if _, ok := c.edges[edgeKey{from, to}]; ok {
		return ErrExists
	}
	if reaches(c.out, to, from) {
		return ErrCycle
	}
	c.edges[edgeKey{from, to}] = struct{}{}
	if c.out[from] == nil {
		c.out[from] = make(map[string]struct{})
	}
	c.out[from][to] = struct{}{}
	if c.in[to] == nil {
		c.in[to] = make(map[string]struct{})
	}
	c.in[to][from] = struct{}{}
	return nil
}

func (c *candidate) deleteEdge(from, to string) error {
	if _, ok := c.edges[edgeKey{from, to}]; !ok {
		return ErrNotFound
	}
	delete(c.edges, edgeKey{from, to})
	delete(c.out[from], to)
	delete(c.in[to], from)
	return nil
}
