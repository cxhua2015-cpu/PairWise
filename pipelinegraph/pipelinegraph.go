package pipelinegraph

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
	options    Options
	generation uint64
	nodes      map[string]struct{}
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
}

func New(options Options) (*Graph, error) {
	if options.MaxNodes <= 0 || options.MaxEdges <= 0 || options.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Graph{
		options: options,
		nodes:   make(map[string]struct{}),
		out:     make(map[string]map[string]struct{}),
		in:      make(map[string]map[string]struct{}),
	}, nil
}

func (g *Graph) Apply(batch Batch) (Result, error) {
	if err := validateBatch(batch, g.options.MaxNameBytes); err != nil {
		return Result{}, err
	}
	if len(batch.Ops) == 0 {
		g.mu.RLock()
		generation := g.generation
		g.mu.RUnlock()
		return Result{Generation: generation}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	nodes := make(map[string]struct{}, len(g.nodes))
	for node := range g.nodes {
		nodes[node] = struct{}{}
	}
	out := make(map[string]map[string]struct{}, len(g.out))
	in := make(map[string]map[string]struct{}, len(g.in))
	for from, neighbors := range g.out {
		out[from] = make(map[string]struct{}, len(neighbors))
		for to := range neighbors {
			out[from][to] = struct{}{}
		}
	}
	for to, neighbors := range g.in {
		in[to] = make(map[string]struct{}, len(neighbors))
		for from := range neighbors {
			in[to][from] = struct{}{}
		}
	}

	for _, op := range batch.Ops {
		switch op.Kind {
		case AddNode:
			if _, exists := nodes[op.From]; exists {
				return Result{}, ErrExists
			}
			nodes[op.From] = struct{}{}
			out[op.From] = make(map[string]struct{})
			in[op.From] = make(map[string]struct{})

		case DeleteNode:
			if _, exists := nodes[op.From]; !exists {
				return Result{}, ErrNotFound
			}
			delete(nodes, op.From)
			for to := range out[op.From] {
				delete(in[to], op.From)
			}
			for from := range in[op.From] {
				delete(out[from], op.From)
			}
			delete(out, op.From)
			delete(in, op.From)

		case AddEdge:
			if _, exists := nodes[op.From]; !exists {
				return Result{}, ErrNotFound
			}
			if _, exists := nodes[op.To]; !exists {
				return Result{}, ErrNotFound
			}
			if _, exists := out[op.From][op.To]; exists {
				return Result{}, ErrExists
			}
			if reaches(op.To, op.From, out) {
				return Result{}, ErrCycle
			}
			addEdgeIndex(op.From, op.To, out, in)

		case DeleteEdge:
			if _, exists := nodes[op.From]; !exists {
				return Result{}, ErrNotFound
			}
			if _, exists := nodes[op.To]; !exists {
				return Result{}, ErrNotFound
			}
			if _, exists := out[op.From][op.To]; !exists {
				return Result{}, ErrNotFound
			}
			delete(out[op.From], op.To)
			delete(in[op.To], op.From)
		}
	}

	if len(nodes) > g.options.MaxNodes || edgeCount(out) > g.options.MaxEdges {
		return Result{}, ErrCapacity
	}

	g.nodes = nodes
	g.out = out
	g.in = in
	g.generation++
	return Result{Generation: g.generation}, nil
}

func (g *Graph) Reachable(source, target string) (bool, error) {
	if !validName(source, g.options.MaxNameBytes) || !validName(target, g.options.MaxNameBytes) {
		return false, ErrInvalidInput
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	if _, exists := g.nodes[source]; !exists {
		return false, ErrNotFound
	}
	if _, exists := g.nodes[target]; !exists {
		return false, ErrNotFound
	}
	return reaches(source, target, g.out), nil
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]string, 0, len(g.nodes))
	for node := range g.nodes {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	edges := make([]Edge, 0, edgeCount(g.out))
	for from, neighbors := range g.out {
		for to := range neighbors {
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

func validateBatch(batch Batch, maxNameBytes int) error {
	for _, op := range batch.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" || !validName(op.From, maxNameBytes) {
				return ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(op.From, maxNameBytes) || !validName(op.To, maxNameBytes) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func validName(name string, maxNameBytes int) bool {
	if name == "" || len(name) > maxNameBytes {
		return false
	}
	for _, r := range name {
		if r > 127 {
			return false
		}
		allowed := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !allowed {
			return false
		}
	}
	return true
}

func addEdgeIndex(from, to string, out, in map[string]map[string]struct{}) {
	if out[from] == nil {
		out[from] = make(map[string]struct{})
	}
	if in[to] == nil {
		in[to] = make(map[string]struct{})
	}
	out[from][to] = struct{}{}
	in[to][from] = struct{}{}
}

func reaches(source, target string, out map[string]map[string]struct{}) bool {
	if source == target {
		return true
	}
	visited := map[string]struct{}{source: {}}
	stack := []string{source}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range out[current] {
			if next == target {
				return true
			}
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = struct{}{}
			stack = append(stack, next)
		}
	}
	return false
}

func edgeCount(out map[string]map[string]struct{}) int {
	count := 0
	for _, neighbors := range out {
		count += len(neighbors)
	}
	return count
}
