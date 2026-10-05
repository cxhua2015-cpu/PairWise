package controlgraph183

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
	maxNodes   int
	maxEdges   int
	maxNameLen int
	generation uint64
	nodes      map[string]struct{}
	edges      map[Edge]struct{}
	out        map[string]map[string]struct{}
}

func New(o Options) (*Graph, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		maxNodes:   o.MaxNodes,
		maxEdges:   o.MaxEdges,
		maxNameLen: o.MaxNameBytes,
		nodes:      make(map[string]struct{}),
		edges:      make(map[Edge]struct{}),
		out:        make(map[string]map[string]struct{}),
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

func (g *Graph) Apply(b Batch) (Result, error) {
	if len(b.Ops) == 0 {
		g.mu.RLock()
		gen := g.generation
		g.mu.RUnlock()
		return Result{Generation: gen}, nil
	}
	// Full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, g.maxNameLen) {
				return Result{}, ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, g.maxNameLen) || !validName(op.To, g.maxNameLen) {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Candidate transaction: copy-on-write overlays rolled back on failure.
	addedNodes := make(map[string]struct{})
	removedNodes := make(map[string]struct{})
	addedEdges := make(map[Edge]struct{})
	removedEdges := make(map[Edge]struct{})

	hasNode := func(n string) bool {
		if _, ok := removedNodes[n]; ok {
			return false
		}
		if _, ok := addedNodes[n]; ok {
			return true
		}
		_, ok := g.nodes[n]
		return ok
	}
	hasEdge := func(e Edge) bool {
		if _, ok := removedEdges[e]; ok {
			return false
		}
		if _, ok := addedEdges[e]; ok {
			return true
		}
		_, ok := g.edges[e]
		return ok
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if hasNode(op.From) {
				return Result{}, ErrExists
			}
			addedNodes[op.From] = struct{}{}
			delete(removedNodes, op.From)
		case DeleteNode:
			if !hasNode(op.From) {
				return Result{}, ErrNotFound
			}
			delete(addedNodes, op.From)
			removedNodes[op.From] = struct{}{}
			// Cascade: drop all incident edges (committed and candidate).
			for e := range addedEdges {
				if e.From == op.From || e.To == op.From {
					delete(addedEdges, e)
				}
			}
			for e := range g.edges {
				if _, gone := removedEdges[e]; gone {
					continue
				}
				if e.From == op.From || e.To == op.From {
					removedEdges[e] = struct{}{}
				}
			}
		case AddEdge:
			e := Edge{From: op.From, To: op.To}
			if !hasNode(op.From) || !hasNode(op.To) {
				return Result{}, ErrNotFound
			}
			if hasEdge(e) {
				return Result{}, ErrExists
			}
			if op.From == op.To || g.reachableLocked(op.To, op.From, addedEdges, removedEdges, removedNodes) {
				return Result{}, ErrCycle
			}
			addedEdges[e] = struct{}{}
			delete(removedEdges, e)
		case DeleteEdge:
			e := Edge{From: op.From, To: op.To}
			if !hasEdge(e) {
				return Result{}, ErrNotFound
			}
			delete(addedEdges, e)
			removedEdges[e] = struct{}{}
		}
	}

	// Capacity checked only on the final candidate state.
	nodeCount := len(g.nodes) + len(addedNodes) - len(removedNodes)
	edgeCount := len(g.edges) + len(addedEdges) - len(removedEdges)
	if nodeCount > g.maxNodes || edgeCount > g.maxEdges {
		return Result{}, ErrCapacity
	}

	// Commit.
	for n := range removedNodes {
		delete(g.nodes, n)
		delete(g.out, n)
	}
	for n := range addedNodes {
		g.nodes[n] = struct{}{}
	}
	for e := range removedEdges {
		delete(g.edges, e)
		if g.out[e.From] != nil {
			delete(g.out[e.From], e.To)
		}
	}
	for e := range addedEdges {
		g.edges[e] = struct{}{}
		if g.out[e.From] == nil {
			g.out[e.From] = make(map[string]struct{})
		}
		g.out[e.From][e.To] = struct{}{}
	}
	g.generation++
	return Result{Generation: g.generation}, nil
}

// reachableLocked reports whether dst is reachable from src in the candidate
// graph (committed state plus pending overlays). Caller holds the lock.
func (g *Graph) reachableLocked(src, dst string, added, removed map[Edge]struct{}, removedNodes map[string]struct{}) bool {
	if src == dst {
		return true
	}
	visited := map[string]struct{}{src: {}}
	stack := []string{src}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for to := range g.out[cur] {
			if _, gone := removed[Edge{From: cur, To: to}]; gone {
				continue
			}
			if _, gone := removedNodes[to]; gone {
				continue
			}
			if to == dst {
				return true
			}
			if _, seen := visited[to]; !seen {
				visited[to] = struct{}{}
				stack = append(stack, to)
			}
		}
		for e := range added {
			if e.From != cur {
				continue
			}
			if e.To == dst {
				return true
			}
			if _, seen := visited[e.To]; !seen {
				visited[e.To] = struct{}{}
				stack = append(stack, e.To)
			}
		}
	}
	return false
}

func (g *Graph) Reachable(from, to string) (bool, error) {
	if !validName(from, g.maxNameLen) || !validName(to, g.maxNameLen) {
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
	visited := map[string]struct{}{from: {}}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for next := range g.out[cur] {
			if next == to {
				return true, nil
			}
			if _, seen := visited[next]; !seen {
				visited[next] = struct{}{}
				queue = append(queue, next)
			}
		}
	}
	return false, nil
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
