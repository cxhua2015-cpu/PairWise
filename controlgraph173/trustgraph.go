package controlgraph173

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
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
	generation uint64
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  o,
		nodes: map[string]struct{}{},
		edges: map[Edge]struct{}{},
		out:   map[string]map[string]struct{}{},
		in:    map[string]map[string]struct{}{},
	}, nil
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
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

// validate checks the batch structurally without reading graph state.
func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
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
	// Candidate transaction: work on clones, commit on success.
	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	edges := make(map[Edge]struct{}, len(g.edges))
	for e := range g.edges {
		edges[e] = struct{}{}
	}
	out := cloneAdj(g.out)
	in := cloneAdj(g.in)

	addNode := func(n string) error {
		if _, ok := nodes[n]; ok {
			return ErrExists
		}
		nodes[n] = struct{}{}
		return nil
	}
	delNode := func(n string) error {
		if _, ok := nodes[n]; !ok {
			return ErrNotFound
		}
		for to := range out[n] {
			delete(edges, Edge{n, to})
			delete(in[to], n)
		}
		for from := range in[n] {
			delete(edges, Edge{from, n})
			delete(out[from], n)
		}
		delete(nodes, n)
		delete(out, n)
		delete(in, n)
		return nil
	}
	addEdge := func(from, to string) error {
		if _, ok := nodes[from]; !ok {
			return ErrNotFound
		}
		if _, ok := nodes[to]; !ok {
			return ErrNotFound
		}
		e := Edge{from, to}
		if _, ok := edges[e]; ok {
			return ErrExists
		}
		if reaches(out, to, from) {
			return ErrCycle
		}
		edges[e] = struct{}{}
		if out[from] == nil {
			out[from] = map[string]struct{}{}
		}
		out[from][to] = struct{}{}
		if in[to] == nil {
			in[to] = map[string]struct{}{}
		}
		in[to][from] = struct{}{}
		return nil
	}
	delEdge := func(from, to string) error {
		e := Edge{from, to}
		if _, ok := edges[e]; !ok {
			return ErrNotFound
		}
		delete(edges, e)
		delete(out[from], to)
		delete(in[to], from)
		return nil
	}

	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = addNode(op.From)
		case DeleteNode:
			err = delNode(op.From)
		case AddEdge:
			err = addEdge(op.From, op.To)
		case DeleteEdge:
			err = delEdge(op.From, op.To)
		}
		if err != nil {
			return Result{}, err
		}
	}
	if len(nodes) > g.opts.MaxNodes || len(edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges, g.out, g.in = nodes, edges, out, in
	g.generation++
	return Result{Generation: g.generation}, nil
}

func cloneAdj(m map[string]map[string]struct{}) map[string]map[string]struct{} {
	c := make(map[string]map[string]struct{}, len(m))
	for k, v := range m {
		nv := make(map[string]struct{}, len(v))
		for x := range v {
			nv[x] = struct{}{}
		}
		c[k] = nv
	}
	return c
}

// reaches reports whether dst is reachable from src via adj (src == dst counts).
func reaches(adj map[string]map[string]struct{}, src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for to := range adj[n] {
			if to == dst {
				return true
			}
			if _, ok := seen[to]; !ok {
				seen[to] = struct{}{}
				stack = append(stack, to)
			}
		}
	}
	return false
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.opts.MaxNameBytes) || !validName(to, g.opts.MaxNameBytes) {
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
