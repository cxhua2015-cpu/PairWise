package topologygraph238

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

// Graph is a concurrency-safe in-memory directed topology graph.
//
// Indexes: nodes is the node set; edges is the edge set keyed by ordered
// (From, To) pair; out is the forward adjacency index (From -> set of To)
// used by cycle detection and Reachable; inbound edges are derived by
// scanning out, keeping memory proportional to |E|.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{}
	generation uint64
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
	}, nil
}

// candidate is a speculative transaction: ops are applied to private copies
// of the indexes and only swapped into the graph on success, so a failed
// batch rolls back as a whole with no observable partial state.
type candidate struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
}

func (g *Graph) newCandidate() *candidate {
	c := &candidate{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, tos := range g.out {
		s := make(map[string]struct{}, len(tos))
		for to := range tos {
			s[to] = struct{}{}
		}
		c.out[from] = s
	}
	return c
}

func (c *candidate) addEdge(from, to string) {
	s := c.out[from]
	if s == nil {
		s = make(map[string]struct{})
		c.out[from] = s
	}
	s[to] = struct{}{}
	c.edges[Edge{From: from, To: to}] = struct{}{}
}

func (c *candidate) deleteEdge(from, to string) {
	if s := c.out[from]; s != nil {
		delete(s, to)
		if len(s) == 0 {
			delete(c.out, from)
		}
	}
	delete(c.edges, Edge{From: from, To: to})
}

func (c *candidate) deleteNode(n string) {
	delete(c.nodes, n)
	for to := range c.out[n] {
		delete(c.edges, Edge{From: n, To: to})
	}
	delete(c.out, n)
	for from, tos := range c.out {
		if _, ok := tos[n]; ok {
			delete(tos, n)
			delete(c.edges, Edge{From: from, To: n})
			if len(tos) == 0 {
				delete(c.out, from)
			}
		}
	}
}

// reachable reports whether dst is reachable from src over the candidate
// adjacency, excluding the edge (skipFrom, skipTo) if it is non-empty.
func (c *candidate) reachable(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range c.out[cur] {
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

// Apply executes a batch atomically: structural validation first, then the
// ops are replayed on a candidate transaction; capacity is checked only at
// the end and any failure discards the whole candidate.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	c := g.newCandidate()
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := c.nodes[op.From]; ok {
				return Result{}, ErrExists
			}
			c.nodes[op.From] = struct{}{}
		case DeleteNode:
			if _, ok := c.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			c.deleteNode(op.From)
		case AddEdge:
			if _, ok := c.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := c.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := c.edges[Edge{From: op.From, To: op.To}]; ok {
				return Result{}, ErrExists
			}
			if c.reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			c.addEdge(op.From, op.To)
		case DeleteEdge:
			if _, ok := c.edges[Edge{From: op.From, To: op.To}]; !ok {
				return Result{}, ErrNotFound
			}
			c.deleteEdge(op.From, op.To)
		default:
			return Result{}, ErrInvalidInput
		}
	}
	if len(c.nodes) > g.opts.MaxNodes || len(c.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges, g.out = c.nodes, c.edges, c.out
	g.generation++
	return Result{Generation: g.generation}, nil
}

// Reachable reports whether dst is reachable from src on a consistent
// snapshot of the current committed state.
func (g *Graph) Reachable(from, to string) (bool, error) {
	if err := g.validName(from); err != nil {
		return false, err
	}
	if err := g.validName(to); err != nil {
		return false, err
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return false, ErrNotFound
	}
	c := &candidate{out: g.out}
	return c.reachable(from, to), nil
}

// Snapshot returns a consistently ordered, fully owned copy of the state.
func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := Snapshot{
		Generation: g.generation,
		Nodes:      make([]string, 0, len(g.nodes)),
		Edges:      make([]Edge, 0, len(g.edges)),
	}
	for n := range g.nodes {
		s.Nodes = append(s.Nodes, n)
	}
	sort.Strings(s.Nodes)
	for e := range g.edges {
		s.Edges = append(s.Edges, e)
	}
	sort.Slice(s.Edges, func(i, j int) bool {
		if s.Edges[i].From != s.Edges[j].From {
			return s.Edges[i].From < s.Edges[j].From
		}
		return s.Edges[i].To < s.Edges[j].To
	})
	return s
}
