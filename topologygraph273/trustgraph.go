package topologygraph273

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
// Indexes: nodes are kept in a hash set; edges are kept in a hash set keyed
// by Edge plus a forward adjacency index (from -> set(to)) used by cycle
// detection and Reachable. A reverse adjacency index (to -> set(from))
// accelerates DeleteNode cascade removal.
type Graph struct {
	mu    sync.RWMutex
	opts  Options
	nodes map[string]struct{}
	edges map[Edge]struct{}
	out   map[string]map[string]struct{}
	in    map[string]map[string]struct{}
	gen   uint64
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
		in:    make(map[string]map[string]struct{}),
	}, nil
}

// validName reports whether n is a non-empty ASCII [a-z0-9-_] name within
// the configured byte limit.
func (g *Graph) validName(n string) bool {
	if n == "" || len(n) > g.opts.MaxNameBytes {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validateOp checks the structural shape of a single op. It is shared by
// ValidateBatch and Apply so both enforce identical semantics.
func (g *Graph) validateOp(op Op) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" {
			return ErrInvalidInput
		}
		if !g.validName(op.From) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !g.validName(op.From) || !g.validName(op.To) || op.From == op.To {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// undo records a reversible mutation for candidate-transaction rollback.
type undo struct {
	node  string // node to restore (DeleteNode) or remove (AddNode)
	edges []Edge // edges to restore (DeleteNode/DeleteEdge) or remove (AddEdge)
	added bool   // true: node/edges were added by the op; false: removed
}

func (g *Graph) addNodeLocked(n string) {
	g.nodes[n] = struct{}{}
}

func (g *Graph) addEdgeLocked(e Edge) {
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

func (g *Graph) removeEdgeLocked(e Edge) {
	delete(g.edges, e)
	delete(g.out[e.From], e.To)
	if len(g.out[e.From]) == 0 {
		delete(g.out, e.From)
	}
	delete(g.in[e.To], e.From)
	if len(g.in[e.To]) == 0 {
		delete(g.in, e.To)
	}
}

// reachableLocked reports whether dst is reachable from src via forward edges.
// Callers must hold at least a read lock.
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

// Apply executes a batch as an atomic candidate transaction: structural
// validation runs first (no state reads), then ops mutate private candidate
// state under the write lock; any failure (including the end-of-batch
// capacity check) rolls the candidate back in reverse order.
func (g *Graph) Apply(b Batch) (Result, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	var undos []undo
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.added {
				if u.node != "" {
					delete(g.nodes, u.node)
				}
				for _, e := range u.edges {
					g.removeEdgeLocked(e)
				}
			} else {
				if u.node != "" {
					g.addNodeLocked(u.node)
				}
				for _, e := range u.edges {
					g.addEdgeLocked(e)
				}
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
			g.addNodeLocked(op.From)
			undos = append(undos, undo{node: op.From, added: true})
		case DeleteNode:
			if _, ok := g.nodes[op.From]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			var removed []Edge
			for to := range g.out[op.From] {
				removed = append(removed, Edge{op.From, to})
			}
			for from := range g.in[op.From] {
				removed = append(removed, Edge{from, op.From})
			}
			for _, e := range removed {
				g.removeEdgeLocked(e)
			}
			delete(g.nodes, op.From)
			undos = append(undos, undo{node: op.From, edges: removed})
		case AddEdge:
			e := Edge{op.From, op.To}
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
			g.addEdgeLocked(e)
			undos = append(undos, undo{edges: []Edge{e}, added: true})
		case DeleteEdge:
			e := Edge{op.From, op.To}
			if _, ok := g.edges[e]; !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			g.removeEdgeLocked(e)
			undos = append(undos, undo{edges: []Edge{e}})
		}
	}

	if len(g.nodes) > g.opts.MaxNodes || len(g.edges) > g.opts.MaxEdges {
		rollback()
		return Result{}, ErrCapacity
	}
	if len(b.Ops) > 0 {
		g.gen++
	}
	return Result{Generation: g.gen}, nil
}

// Reachable reports whether dst is reachable from src on a consistent
// snapshot of the current committed state.
func (g *Graph) Reachable(src, dst string) (bool, error) {
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

// Snapshot returns a stably sorted, fully detached copy of the state.
func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := Snapshot{
		Generation: g.gen,
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
