package topologygraph433

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
	adj        map[string]map[string]struct{}
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
		adj:   make(map[string]map[string]struct{}),
	}, nil
}

func (g *Graph) Apply(b Batch) (Result, error) {
	if err := validateBatch(b, g.opts.MaxNameBytes); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, nil
	}
	w := g.lockedCloneState()
	if err := w.applyOps(b.Ops); err != nil {
		return Result{}, err
	}
	if len(w.nodes) > g.opts.MaxNodes || len(w.edges) > g.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	g.nodes, g.edges, g.adj = w.nodes, w.edges, w.adj
	g.generation++
	return Result{Generation: g.generation}, nil
}

// applyOps mutates the receiver state by applying ops in order.
func (g *Graph) applyOps(ops []Op) error {
	for _, op := range ops {
		switch op.Kind {
		case AddNode:
			if _, ok := g.nodes[op.From]; ok {
				return ErrExists
			}
			g.nodes[op.From] = struct{}{}
			g.adj[op.From] = make(map[string]struct{})
		case DeleteNode:
			if _, ok := g.nodes[op.From]; !ok {
				return ErrNotFound
			}
			for to := range g.adj[op.From] {
				delete(g.edges, Edge{op.From, to})
			}
			delete(g.adj, op.From)
			for _, outs := range g.adj {
				delete(outs, op.From)
			}
			for e := range g.edges {
				if e.To == op.From {
					delete(g.edges, e)
				}
			}
			delete(g.nodes, op.From)
		case AddEdge:
			if _, ok := g.nodes[op.From]; !ok {
				return ErrNotFound
			}
			if _, ok := g.nodes[op.To]; !ok {
				return ErrNotFound
			}
			e := Edge{op.From, op.To}
			if _, ok := g.edges[e]; ok {
				return ErrExists
			}
			if g.reachableLocked(op.To, op.From) {
				return ErrCycle
			}
			g.edges[e] = struct{}{}
			g.adj[op.From][op.To] = struct{}{}
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := g.edges[e]; !ok {
				return ErrNotFound
			}
			delete(g.edges, e)
			delete(g.adj[op.From], op.To)
		}
	}
	return nil
}

func (g *Graph) reachableLocked(from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]struct{}{from: {}}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range g.adj[n] {
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
	return g.reachableLocked(from, to), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.snapshotLocked()
}

func (g *Graph) snapshotLocked() Snapshot {
	s := Snapshot{Generation: g.generation}
	if len(g.nodes) > 0 {
		s.Nodes = make([]string, 0, len(g.nodes))
		for n := range g.nodes {
			s.Nodes = append(s.Nodes, n)
		}
		sort.Strings(s.Nodes)
	}
	if len(g.edges) > 0 {
		s.Edges = make([]Edge, 0, len(g.edges))
		for e := range g.edges {
			s.Edges = append(s.Edges, e)
		}
		sort.Slice(s.Edges, func(i, j int) bool {
			if s.Edges[i].From != s.Edges[j].From {
				return s.Edges[i].From < s.Edges[j].From
			}
			return s.Edges[i].To < s.Edges[j].To
		})
	}
	return s
}

// lockedCloneState copies nodes/edges/adj; caller must hold the lock.
func (g *Graph) lockedCloneState() *Graph {
	w := &Graph{
		opts:  g.opts,
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		adj:   make(map[string]map[string]struct{}, len(g.adj)),
	}
	for n := range g.nodes {
		w.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		w.edges[e] = struct{}{}
	}
	for n, outs := range g.adj {
		no := make(map[string]struct{}, len(outs))
		for to := range outs {
			no[to] = struct{}{}
		}
		w.adj[n] = no
	}
	return w
}
