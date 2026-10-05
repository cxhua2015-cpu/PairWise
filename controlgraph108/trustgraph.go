package controlgraph108

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
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
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
		edges:    make(map[Edge]struct{}),
		out:      make(map[string]map[string]struct{}),
		in:       make(map[string]map[string]struct{}),
		maxNodes: o.MaxNodes,
		maxEdges: o.MaxEdges,
		maxName:  o.MaxNameBytes,
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

func (g *Graph) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if !validName(op.From, g.maxName) || op.To != "" {
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
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.validate(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
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
	g.nodes, g.edges, g.out, g.in = c.nodes, c.edges, c.out, c.in
	g.generation++
	return Result{Generation: g.generation}, nil
}

type candidate struct {
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
}

func (g *Graph) candidate() *candidate {
	c := &candidate{
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
		in:    make(map[string]map[string]struct{}, len(g.in)),
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, set := range g.out {
		s := make(map[string]struct{}, len(set))
		for to := range set {
			s[to] = struct{}{}
		}
		c.out[from] = s
	}
	for to, set := range g.in {
		s := make(map[string]struct{}, len(set))
		for from := range set {
			s[from] = struct{}{}
		}
		c.in[to] = s
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
	delete(c.nodes, n)
	for to := range c.out[n] {
		delete(c.edges, Edge{From: n, To: to})
		delete(c.in[to], n)
		if len(c.in[to]) == 0 {
			delete(c.in, to)
		}
	}
	delete(c.out, n)
	for from := range c.in[n] {
		delete(c.edges, Edge{From: from, To: n})
		delete(c.out[from], n)
		if len(c.out[from]) == 0 {
			delete(c.out, from)
		}
	}
	delete(c.in, n)
	return nil
}

func (c *candidate) reachable(from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range c.out[n] {
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

func (c *candidate) addEdge(from, to string) error {
	if _, ok := c.nodes[from]; !ok {
		return ErrNotFound
	}
	if _, ok := c.nodes[to]; !ok {
		return ErrNotFound
	}
	e := Edge{From: from, To: to}
	if _, ok := c.edges[e]; ok {
		return ErrExists
	}
	if c.reachable(to, from) {
		return ErrCycle
	}
	c.edges[e] = struct{}{}
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
	e := Edge{From: from, To: to}
	if _, ok := c.edges[e]; !ok {
		return ErrNotFound
	}
	delete(c.edges, e)
	delete(c.out[from], to)
	if len(c.out[from]) == 0 {
		delete(c.out, from)
	}
	delete(c.in[to], from)
	if len(c.in[to]) == 0 {
		delete(c.in, to)
	}
	return nil
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
	c := &candidate{out: g.out}
	return c.reachable(from, to), nil
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
