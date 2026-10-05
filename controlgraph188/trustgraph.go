package controlgraph188

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
	nodes      map[string]struct{}
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
	edges      int
	generation uint64
	maxNodes   int
	maxEdges   int
	maxName    int
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		nodes:    make(map[string]struct{}),
		out:      make(map[string]map[string]struct{}),
		in:       make(map[string]map[string]struct{}),
		maxNodes: o.MaxNodes,
		maxEdges: o.MaxEdges,
		maxName:  o.MaxNameBytes,
	}, nil
}

func validName(s string, max int) bool {
	if s == "" || len(s) > max {
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

func (g *Graph) Apply(b Batch) (Result, error) {
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

	nodes := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		nodes[n] = struct{}{}
	}
	out := make(map[string]map[string]struct{}, len(g.out))
	for from, set := range g.out {
		s := make(map[string]struct{}, len(set))
		for to := range set {
			s[to] = struct{}{}
		}
		out[from] = s
	}
	in := make(map[string]map[string]struct{}, len(g.in))
	for to, set := range g.in {
		s := make(map[string]struct{}, len(set))
		for from := range set {
			s[from] = struct{}{}
		}
		in[to] = s
	}
	edges := g.edges

	addNode := func(n string) error {
		if _, ok := nodes[n]; ok {
			return ErrExists
		}
		nodes[n] = struct{}{}
		return nil
	}
	removeEdge := func(from, to string) {
		delete(out[from], to)
		if len(out[from]) == 0 {
			delete(out, from)
		}
		delete(in[to], from)
		if len(in[to]) == 0 {
			delete(in, to)
		}
		edges--
	}
	deleteNode := func(n string) error {
		if _, ok := nodes[n]; !ok {
			return ErrNotFound
		}
		for to := range out[n] {
			delete(in[to], n)
			if len(in[to]) == 0 {
				delete(in, to)
			}
			edges--
		}
		delete(out, n)
		for from := range in[n] {
			delete(out[from], n)
			if len(out[from]) == 0 {
				delete(out, from)
			}
			edges--
		}
		delete(in, n)
		delete(nodes, n)
		return nil
	}
	reaches := func(from, to string) bool {
		if from == to {
			return true
		}
		seen := map[string]struct{}{from: {}}
		stack := []string{from}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for next := range out[cur] {
				if next == to {
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
	addEdge := func(from, to string) error {
		if _, ok := nodes[from]; !ok {
			return ErrNotFound
		}
		if _, ok := nodes[to]; !ok {
			return ErrNotFound
		}
		if _, ok := out[from][to]; ok {
			return ErrExists
		}
		if reaches(to, from) {
			return ErrCycle
		}
		if out[from] == nil {
			out[from] = make(map[string]struct{})
		}
		out[from][to] = struct{}{}
		if in[to] == nil {
			in[to] = make(map[string]struct{})
		}
		in[to][from] = struct{}{}
		edges++
		return nil
	}
	deleteEdge := func(from, to string) error {
		if _, ok := out[from][to]; !ok {
			return ErrNotFound
		}
		removeEdge(from, to)
		return nil
	}

	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case AddNode:
			err = addNode(op.From)
		case DeleteNode:
			err = deleteNode(op.From)
		case AddEdge:
			err = addEdge(op.From, op.To)
		case DeleteEdge:
			err = deleteEdge(op.From, op.To)
		}
		if err != nil {
			return Result{}, err
		}
	}

	if len(nodes) > g.maxNodes || edges > g.maxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes = nodes
	g.out = out
	g.in = in
	g.edges = edges
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
	if from == to {
		return true, nil
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range g.out[cur] {
			if next == to {
				return true, nil
			}
			if _, ok := seen[next]; !ok {
				seen[next] = struct{}{}
				stack = append(stack, next)
			}
		}
	}
	return false, nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	nodes := make([]string, 0, len(g.nodes))
	for n := range g.nodes {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	edges := make([]Edge, 0, g.edges)
	for from, set := range g.out {
		for to := range set {
			edges = append(edges, Edge{From: from, To: to})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return Snapshot{Generation: g.generation, Nodes: nodes, Edges: edges}
}
