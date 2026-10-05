package ownershipgraph

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

type Graph struct {
	mu      sync.RWMutex
	maxN    int
	maxE    int
	maxName int
	nodes   map[string]struct{}
	out     map[string]map[string]struct{} // from -> set of to
	in      map[string]map[string]struct{} // to -> set of from
	edges   int
	gen     uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxN:    o.MaxNodes,
		maxE:    o.MaxEdges,
		maxName: o.MaxNameBytes,
		nodes:   make(map[string]struct{}),
		out:     make(map[string]map[string]struct{}),
		in:      make(map[string]map[string]struct{}),
	}, nil
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
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

// validateOp checks only the structural shape of an op: known kind,
// no unexpected fields, and well-formed names. It never reads graph state.
func (g *Graph) validateOp(op Op) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" {
			return ErrInvalidInput
		}
		if !validName(op.From, g.maxName) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(op.From, g.maxName) || !validName(op.To, g.maxName) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching state.
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		g.mu.RLock()
		gen := g.gen
		g.mu.RUnlock()
		return Result{Generation: gen}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Phase 2: candidate transaction on a private copy of the state.
	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	out := make(map[string]map[string]struct{}, len(g.out))
	for f, set := range g.out {
		s := make(map[string]struct{}, len(set))
		for t := range set {
			s[t] = struct{}{}
		}
		out[f] = s
	}
	in := make(map[string]map[string]struct{}, len(g.in))
	for t, set := range g.in {
		s := make(map[string]struct{}, len(set))
		for f := range set {
			s[f] = struct{}{}
		}
		in[t] = s
	}
	edgeCount := g.edges

	addEdge := func(from, to string) {
		if out[from] == nil {
			out[from] = make(map[string]struct{})
		}
		out[from][to] = struct{}{}
		if in[to] == nil {
			in[to] = make(map[string]struct{})
		}
		in[to][from] = struct{}{}
		edgeCount++
	}
	delEdge := func(from, to string) {
		delete(out[from], to)
		if len(out[from]) == 0 {
			delete(out, from)
		}
		delete(in[to], from)
		if len(in[to]) == 0 {
			delete(in, to)
		}
		edgeCount--
	}
	reaches := func(from, to string) bool {
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

	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := nodes[op.From]; ok {
				return Result{}, ErrExists
			}
			nodes[op.From] = struct{}{}
		case DeleteNode:
			if _, ok := nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			for to := range out[op.From] {
				delEdge(op.From, to)
			}
			// copy: delEdge mutates in[op.From]
			srcs := make([]string, 0, len(in[op.From]))
			for from := range in[op.From] {
				srcs = append(srcs, from)
			}
			for _, from := range srcs {
				delEdge(from, op.From)
			}
			delete(nodes, op.From)
		case AddEdge:
			if _, ok := nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := out[op.From][op.To]; ok {
				return Result{}, ErrExists
			}
			if reaches(op.To, op.From) {
				return Result{}, ErrCycle
			}
			addEdge(op.From, op.To)
		case DeleteEdge:
			if _, ok := out[op.From][op.To]; !ok {
				return Result{}, ErrNotFound
			}
			delEdge(op.From, op.To)
		}
	}

	// Phase 3: capacity checked only on the final state of the batch.
	if len(nodes) > g.maxN || edgeCount > g.maxE {
		return Result{}, ErrCapacity
	}

	// Commit.
	g.nodes = nodes
	g.out = out
	g.in = in
	g.edges = edgeCount
	g.gen++
	return Result{Generation: g.gen}, nil
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
	if from == to {
		return true, nil
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range g.out[n] {
			if m == to {
				return true, nil
			}
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				stack = append(stack, m)
			}
		}
	}
	return false, nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := Snapshot{Generation: g.gen, Nodes: make([]string, 0, len(g.nodes)), Edges: make([]Edge, 0, g.edges)}
	for n := range g.nodes {
		s.Nodes = append(s.Nodes, n)
	}
	sort.Strings(s.Nodes)
	for f, set := range g.out {
		for t := range set {
			s.Edges = append(s.Edges, Edge{From: f, To: t})
		}
	}
	sort.Slice(s.Edges, func(i, j int) bool {
		if s.Edges[i].From != s.Edges[j].From {
			return s.Edges[i].From < s.Edges[j].From
		}
		return s.Edges[i].To < s.Edges[j].To
	})
	return s
}
