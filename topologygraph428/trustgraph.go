package topologygraph428

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

// topologyState is the mutable graph content owned by a single Graph (or a
// short-lived candidate during Apply/Preview). The mutex never guards it
// directly; ownership transfer happens while the parent mutex is held.
type topologyState struct {
	nodes map[string]struct{}
	edges map[edgeKey]struct{}
	out   map[string]map[string]struct{}
}

func newState() *topologyState {
	return &topologyState{
		nodes: map[string]struct{}{},
		edges: map[edgeKey]struct{}{},
		out:   map[string]map[string]struct{}{},
	}
}

func (s *topologyState) clone() *topologyState {
	c := newState()
	for n := range s.nodes {
		c.nodes[n] = struct{}{}
	}
	for k := range s.edges {
		c.edges[k] = struct{}{}
	}
	for from, tos := range s.out {
		cp := make(map[string]struct{}, len(tos))
		for to := range tos {
			cp[to] = struct{}{}
		}
		c.out[from] = cp
	}
	return c
}

// reaches reports whether start can reach target following directed edges.
func (s *topologyState) reaches(start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	stack := []string{start}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range s.out[cur] {
			if next == target {
				return true
			}
			if _, ok := seen[next]; ok {
				continue
			}
			seen[next] = struct{}{}
			stack = append(stack, next)
		}
	}
	return false
}

func (s *topologyState) removeNode(name string) {
	delete(s.nodes, name)
	for k := range s.edges {
		if k.from == name || k.to == name {
			delete(s.edges, k)
		}
	}
	delete(s.out, name)
	for _, tos := range s.out {
		delete(tos, name)
	}
}

// apply mutates the candidate state with full batch semantics. Structural
// validation must already have happened; only state-dependent errors and the
// final capacity check are evaluated here. On error the state is a partially
// mutated throwaway copy, which callers discard to achieve atomic rollback.
func (s *topologyState) apply(b Batch, opts Options) error {
	for _, op := range b.Ops {
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
			s.removeNode(op.From)
		case AddEdge:
			if _, ok := s.nodes[op.From]; !ok {
				return ErrNotFound
			}
			if _, ok := s.nodes[op.To]; !ok {
				return ErrNotFound
			}
			key := edgeKey{op.From, op.To}
			if _, ok := s.edges[key]; ok {
				return ErrExists
			}
			if s.reaches(op.To, op.From) {
				return ErrCycle
			}
			s.edges[key] = struct{}{}
			tos := s.out[op.From]
			if tos == nil {
				tos = map[string]struct{}{}
				s.out[op.From] = tos
			}
			tos[op.To] = struct{}{}
		case DeleteEdge:
			key := edgeKey{op.From, op.To}
			if _, ok := s.edges[key]; !ok {
				return ErrNotFound
			}
			delete(s.edges, key)
			delete(s.out[op.From], op.To)
		}
	}
	if len(s.nodes) > opts.MaxNodes || len(s.edges) > opts.MaxEdges {
		return ErrCapacity
	}
	return nil
}

func (s *topologyState) snapshot(generation uint64) Snapshot {
	nodes := make([]string, 0, len(s.nodes))
	for n := range s.nodes {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	edges := make([]Edge, 0, len(s.edges))
	for k := range s.edges {
		edges = append(edges, Edge{From: k.from, To: k.to})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return Snapshot{Generation: generation, Nodes: nodes, Edges: edges}
}

// Graph is a concurrency-safe in-memory directed control topology. All reads
// observe one linearizable state; Apply commits batches atomically.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	generation uint64
	state      *topologyState
}

func New(opts Options) (*Graph, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{opts: opts, state: newState()}, nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	candidate := g.state.clone()
	if err := candidate.apply(b, g.opts); err != nil {
		return Result{}, err
	}
	if len(b.Ops) > 0 {
		g.generation++
	}
	g.state = candidate
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.opts.MaxNameBytes) || !validName(to, g.opts.MaxNameBytes) {
		return false, ErrInvalidInput
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.state.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.state.nodes[to]; !ok {
		return false, ErrNotFound
	}
	return g.state.reaches(from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.state.snapshot(g.generation)
}
