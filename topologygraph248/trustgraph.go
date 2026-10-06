package topologygraph248

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
// All public methods are safe for concurrent use.
type Graph struct {
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
	generation uint64
}

func New(opts Options) (*Graph, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		opts:  opts,
		nodes: make(map[string]struct{}),
		edges: make(map[Edge]struct{}),
		out:   make(map[string]map[string]struct{}),
		in:    make(map[string]map[string]struct{}),
	}, nil
}

// undo records an inverse action so a failed batch can be rolled back.
type undo struct {
	addNode *string
	delNode *string
	addEdge *Edge
	delEdge *Edge
}

func (g *Graph) addNode(name string) {
	g.nodes[name] = struct{}{}
}

func (g *Graph) removeNode(name string) {
	delete(g.nodes, name)
}

func (g *Graph) addEdge(e Edge) {
	g.edges[e] = struct{}{}
	if g.out[e.From] == nil {
		g.out[e.From] = make(map[string]struct{})
	}
	g.out[e.From][e.To] = struct{}{}
	if g.in[e.To] == nil {
		g.in[e.To] = make(map[string]struct{})
	}
	g.in[e.To][e.From] = struct{}{}
}

func (g *Graph) removeEdge(e Edge) {
	delete(g.edges, e)
	if m := g.out[e.From]; m != nil {
		delete(m, e.To)
		if len(m) == 0 {
			delete(g.out, e.From)
		}
	}
	if m := g.in[e.To]; m != nil {
		delete(m, e.From)
		if len(m) == 0 {
			delete(g.in, e.To)
		}
	}
}

// reachableLocked reports whether dst is reachable from src. Caller holds a lock.
func (g *Graph) reachableLocked(src, dst string) bool {
	if src == dst {
		return true
	}
	seen := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range g.out[cur] {
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

// Apply executes the batch atomically. On any failure the graph is left
// unchanged. Capacity limits are only checked at the end of the batch.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	var log []undo
	rollback := func() {
		for i := len(log) - 1; i >= 0; i-- {
			u := log[i]
			if u.addNode != nil {
				g.addNode(*u.addNode)
			}
			if u.delNode != nil {
				g.removeNode(*u.delNode)
			}
			if u.addEdge != nil {
				g.addEdge(*u.addEdge)
			}
			if u.delEdge != nil {
				g.removeEdge(*u.delEdge)
			}
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := g.nodes[op.From]; ok {
				rollback()
				return Result{}, ErrExists
			}
			g.addNode(op.From)
			name := op.From
			log = append(log, undo{delNode: &name})
		case DeleteNode:
			if _, ok := g.nodes[op.From]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			// Remove all incident edges together with the node.
			var removed []Edge
			for to := range g.out[op.From] {
				e := Edge{From: op.From, To: to}
				g.removeEdge(e)
				removed = append(removed, e)
			}
			for from := range g.in[op.From] {
				e := Edge{From: from, To: op.From}
				g.removeEdge(e)
				removed = append(removed, e)
			}
			g.removeNode(op.From)
			name := op.From
			u := undo{addNode: &name}
			// Re-add edges before the node on rollback: record edges as
			// separate undo entries applied after the node restore.
			log = append(log, u)
			for i := len(removed) - 1; i >= 0; i-- {
				e := removed[i]
				log = append(log, undo{addEdge: &e})
			}
		case AddEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := g.nodes[op.From]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			if _, ok := g.nodes[op.To]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			if _, ok := g.edges[e]; ok {
				rollback()
				return Result{}, ErrExists
			}
			if g.reachableLocked(op.To, op.From) {
				rollback()
				return Result{}, ErrCycle
			}
			g.addEdge(e)
			log = append(log, undo{delEdge: &e})
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
			if _, ok := g.edges[e]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			g.removeEdge(e)
			log = append(log, undo{addEdge: &e})
		}
	}

	if len(g.nodes) > g.opts.MaxNodes || len(g.edges) > g.opts.MaxEdges {
		rollback()
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		g.generation++
	}
	return Result{Generation: g.generation}, nil
}

// Reachable reports whether dst is reachable from src using a consistent
// snapshot of the graph.
func (g *Graph) Reachable(src, dst string) (bool, error) {
	if !validName(src, g.opts.MaxNameBytes) || !validName(dst, g.opts.MaxNameBytes) {
		return false, ErrInvalidInput
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if _, ok := g.nodes[src]; !ok {
		return false, ErrNotFound
	}
	if _, ok := g.nodes[dst]; !ok {
		return false, ErrNotFound
	}
	return g.reachableLocked(src, dst), nil
}

// Snapshot returns a stably sorted, fully detached view of the graph.
func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	snap := Snapshot{
		Generation: g.generation,
		Nodes:      make([]string, 0, len(g.nodes)),
		Edges:      make([]Edge, 0, len(g.edges)),
	}
	for n := range g.nodes {
		snap.Nodes = append(snap.Nodes, n)
	}
	for e := range g.edges {
		snap.Edges = append(snap.Edges, e)
	}
	sort.Strings(snap.Nodes)
	sort.Slice(snap.Edges, func(i, j int) bool {
		if snap.Edges[i].From != snap.Edges[j].From {
			return snap.Edges[i].From < snap.Edges[j].From
		}
		return snap.Edges[i].To < snap.Edges[j].To
	})
	return snap
}
