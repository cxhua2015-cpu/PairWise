package topologygraph438

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
// Graph is a concurrency-safe in-memory control topology graph.
type Graph struct {
	mu         sync.RWMutex
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	generation uint64
	opts       Options
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		opts:  o,
	}, nil
}

func validName(s string, maxBytes int) bool {
	if s == "" || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validateOp checks the structural rules of a single op without reading state.
func validateOp(op Op, maxNameBytes int) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" || !validName(op.From, maxNameBytes) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(op.From, maxNameBytes) || !validName(op.To, maxNameBytes) || op.From == op.To {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// validateStructural checks the whole batch structurally before any state is read.
func validateStructural(b Batch, maxNameBytes int) error {
	for _, op := range b.Ops {
		if err := validateOp(op, maxNameBytes); err != nil {
			return err
		}
	}
	return nil
}

// reaches reports whether dst is reachable from src in the given edge set.
func reaches(edges map[Edge]struct{}, src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for e := range edges {
			if e.From != n {
				continue
			}
			if e.To == dst {
				return true
			}
			if _, ok := seen[e.To]; !ok {
				seen[e.To] = struct{}{}
				stack = append(stack, e.To)
			}
		}
	}
	return false
}

// applyOps simulates the batch on candidate state, returning state errors in op order.
func applyOps(nodes map[string]struct{}, edges map[Edge]struct{}, ops []Op) error {
	for _, op := range ops {
		switch op.Kind {
		case AddNode:
			if _, ok := nodes[op.From]; ok {
				return ErrExists
			}
			nodes[op.From] = struct{}{}
		case DeleteNode:
			if _, ok := nodes[op.From]; !ok {
				return ErrNotFound
			}
			delete(nodes, op.From)
			for e := range edges {
				if e.From == op.From || e.To == op.From {
					delete(edges, e)
				}
			}
		case AddEdge:
			if _, ok := nodes[op.From]; !ok {
				return ErrNotFound
			}
			if _, ok := nodes[op.To]; !ok {
				return ErrNotFound
			}
			e := Edge{From: op.From, To: op.To}
			if _, ok := edges[e]; ok {
				return ErrExists
			}
			if reaches(edges, op.To, op.From) {
				return ErrCycle
			}
			edges[e] = struct{}{}
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := edges[e]; !ok {
				return ErrNotFound
			}
			delete(edges, e)
		}
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := validateStructural(b, g.opts.MaxNameBytes); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	edges := make(map[Edge]struct{}, len(g.edges))
	for e := range g.edges {
		edges[e] = struct{}{}
	}
	if err := applyOps(nodes, edges, b.Ops); err != nil {
		return Result{}, err
	}
	if len(nodes) > g.opts.MaxNodes || len(edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes = nodes
	g.edges = edges
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[to]; !ok {
		return false, ErrNotFound
	}
	return reaches(g.edges, from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.snapshotLocked()
}

func (g *Graph) snapshotLocked() Snapshot {
	s := Snapshot{
		Generation: g.generation,
		Nodes:      make([]string, 0, len(g.nodes)),
		Edges:      make([]Edge, 0, len(g.edges)),
	}
	for n := range g.nodes {
		s.Nodes = append(s.Nodes, n)
	}
	for e := range g.edges {
		s.Edges = append(s.Edges, e)
	}
	sort.Strings(s.Nodes)
	sort.Slice(s.Edges, func(i, j int) bool {
		if s.Edges[i].From != s.Edges[j].From {
			return s.Edges[i].From < s.Edges[j].From
		}
		return s.Edges[i].To < s.Edges[j].To
	})
	return s
}
