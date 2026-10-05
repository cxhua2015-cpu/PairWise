package dagstore

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrCycle          = errors.New("cycle")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct{ MaxNodes, MaxEdges, MaxNameBytes, MaxPayloadBytes, MaxTotalPayloadBytes int }
type OpKind uint8

const (
	AddNode OpKind = iota + 1
	UpdateNode
	DeleteNode
	AddEdge
	RemoveEdge
)

type Op struct {
	Kind           OpKind
	Name, From, To string
	Payload        []byte
}
type Batch struct{ Ops []Op }
type Node struct {
	Name     string
	Payload  []byte
	Revision uint64
}
type Edge struct {
	From, To string
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	ChangedNodes         []Node
	ChangedEdges         []Edge
}
type Snapshot struct {
	Generation, NextRevision uint64
	Nodes                    []Node
	Edges                    []Edge
}

type edgeKey struct{ from, to string }

type nodeState struct {
	payload  []byte
	revision uint64
}

type Store struct {
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]*nodeState
	edges      map[edgeKey]uint64
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
	payloadSum int
	generation uint64
	revision   uint64
}

func New(o Options) (*Store, error) {
	if o.MaxNodes <= 0 || o.MaxEdges <= 0 || o.MaxNameBytes <= 0 ||
		o.MaxPayloadBytes <= 0 || o.MaxTotalPayloadBytes <= 0 ||
		o.MaxPayloadBytes > o.MaxTotalPayloadBytes {
		return nil, ErrInvalidOptions
	}
	return &Store{
		opts:  o,
		nodes: map[string]*nodeState{},
		edges: map[edgeKey]uint64{},
		out:   map[string]map[string]struct{}{},
		in:    map[string]map[string]struct{}{},
	}, nil
}

func validName(n string, max int) bool {
	if n == "" || len(n) > max {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '/' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func (s *Store) validate(op Op) error {
	switch op.Kind {
	case AddNode, UpdateNode:
		if !validName(op.Name, s.opts.MaxNameBytes) || op.Payload == nil ||
			len(op.Payload) > s.opts.MaxPayloadBytes {
			return ErrInvalidInput
		}
	case DeleteNode:
		if !validName(op.Name, s.opts.MaxNameBytes) || op.Payload != nil {
			return ErrInvalidInput
		}
	case AddEdge, RemoveEdge:
		if op.Name != "" || op.Payload != nil ||
			!validName(op.From, s.opts.MaxNameBytes) ||
			!validName(op.To, s.opts.MaxNameBytes) || op.From == op.To {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// createsCycle reports whether adding from->to closes a directed cycle,
// i.e. whether `from` is already reachable from `to`.
func (s *Store) reachableLocked(from, to string) bool {
	if from == to {
		return true
	}
	seen := map[string]bool{from: true}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range s.out[n] {
			if m == to {
				return true
			}
			if !seen[m] {
				seen[m] = true
				stack = append(stack, m)
			}
		}
	}
	return false
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: structural validation only, no state reads.
	for _, op := range b.Ops {
		if err := s.validate(op); err != nil {
			return Result{}, err
		}
	}

	// Phase 2: execute on isolated candidate copies.
	nodes := make(map[string]*nodeState, len(s.nodes))
	for k, v := range s.nodes {
		nodes[k] = v
	}
	edges := make(map[edgeKey]uint64, len(s.edges))
	for k, v := range s.edges {
		edges[k] = v
	}
	out := make(map[string]map[string]struct{}, len(s.out))
	for k, v := range s.out {
		m := make(map[string]struct{}, len(v))
		for x := range v {
			m[x] = struct{}{}
		}
		out[k] = m
	}
	in := make(map[string]map[string]struct{}, len(s.in))
	for k, v := range s.in {
		m := make(map[string]struct{}, len(v))
		for x := range v {
			m[x] = struct{}{}
		}
		in[k] = m
	}
	payloadSum := s.payloadSum
	rev := s.revision

	changedNodes := map[string]Node{}
	changedEdges := map[edgeKey]Edge{}

	reachable := func(from, to string) bool {
		if from == to {
			return true
		}
		seen := map[string]bool{from: true}
		stack := []string{from}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for m := range out[n] {
				if m == to {
					return true
				}
				if !seen[m] {
					seen[m] = true
					stack = append(stack, m)
				}
			}
		}
		return false
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode:
			if _, ok := nodes[op.Name]; ok {
				return Result{}, ErrConflict
			}
			rev++
			p := append([]byte(nil), op.Payload...)
			nodes[op.Name] = &nodeState{payload: p, revision: rev}
			payloadSum += len(p)
			changedNodes[op.Name] = Node{Name: op.Name, Payload: p, Revision: rev}
		case UpdateNode:
			old, ok := nodes[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			rev++
			p := append([]byte(nil), op.Payload...)
			payloadSum += len(p) - len(old.payload)
			nodes[op.Name] = &nodeState{payload: p, revision: rev}
			changedNodes[op.Name] = Node{Name: op.Name, Payload: p, Revision: rev}
		case DeleteNode:
			old, ok := nodes[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			if len(out[op.Name]) > 0 || len(in[op.Name]) > 0 {
				return Result{}, ErrConflict
			}
			rev++
			payloadSum -= len(old.payload)
			delete(nodes, op.Name)
			delete(changedNodes, op.Name)
		case AddEdge:
			k := edgeKey{op.From, op.To}
			if _, ok := nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := edges[k]; ok {
				return Result{}, ErrConflict
			}
			if reachable(op.To, op.From) {
				return Result{}, ErrCycle
			}
			rev++
			edges[k] = rev
			if out[op.From] == nil {
				out[op.From] = map[string]struct{}{}
			}
			out[op.From][op.To] = struct{}{}
			if in[op.To] == nil {
				in[op.To] = map[string]struct{}{}
			}
			in[op.To][op.From] = struct{}{}
			changedEdges[k] = Edge{From: op.From, To: op.To, Revision: rev}
		case RemoveEdge:
			k := edgeKey{op.From, op.To}
			if _, ok := edges[k]; !ok {
				return Result{}, ErrNotFound
			}
			rev++
			delete(edges, k)
			delete(out[op.From], op.To)
			delete(in[op.To], op.From)
			delete(changedEdges, k)
		}
	}

	// Phase 3: final capacity checks.
	if len(nodes) > s.opts.MaxNodes || len(edges) > s.opts.MaxEdges ||
		payloadSum > s.opts.MaxTotalPayloadBytes {
		return Result{}, ErrCapacity
	}

	// Commit. Candidate payloads were already deep-copied on write.
	s.nodes = nodes
	s.edges = edges
	s.out = out
	s.in = in
	s.payloadSum = payloadSum
	s.revision = rev
	if len(b.Ops) > 0 {
		s.generation++
	}

	res := Result{Generation: s.generation, Revision: rev}
	names := make([]string, 0, len(changedNodes))
	for n := range changedNodes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		cn := changedNodes[n]
		res.ChangedNodes = append(res.ChangedNodes, Node{Name: n, Payload: append([]byte(nil), cn.Payload...), Revision: cn.Revision})
	}
	eks := make([]edgeKey, 0, len(changedEdges))
	for k := range changedEdges {
		eks = append(eks, k)
	}
	sort.Slice(eks, func(i, j int) bool {
		if eks[i].from != eks[j].from {
			return eks[i].from < eks[j].from
		}
		return eks[i].to < eks[j].to
	})
	for _, k := range eks {
		res.ChangedEdges = append(res.ChangedEdges, changedEdges[k])
	}
	return res, nil
}

func (s *Store) Reachable(from, to string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !validName(from, s.opts.MaxNameBytes) || !validName(to, s.opts.MaxNameBytes) {
		return false, ErrInvalidInput
	}
	if _, ok := s.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := s.nodes[to]; !ok {
		return false, ErrNotFound
	}
	return s.reachableLocked(from, to), nil
}

func (s *Store) Topological() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	indeg := make(map[string]int, len(s.nodes))
	for n := range s.nodes {
		indeg[n] = 0
	}
	for k := range s.edges {
		indeg[k.to]++
	}
	avail := make([]string, 0, len(s.nodes))
	for n, d := range indeg {
		if d == 0 {
			avail = append(avail, n)
		}
	}
	sort.Strings(avail)
	order := make([]string, 0, len(s.nodes))
	for len(avail) > 0 {
		n := avail[0]
		avail = avail[1:]
		order = append(order, n)
		var next []string
		for m := range s.out[n] {
			indeg[m]--
			if indeg[m] == 0 {
				next = append(next, m)
			}
		}
		if len(next) > 0 {
			avail = append(avail, next...)
			sort.Strings(avail)
		}
	}
	return order
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{Generation: s.generation, NextRevision: s.revision + 1}
	for n, st := range s.nodes {
		snap.Nodes = append(snap.Nodes, Node{Name: n, Payload: append([]byte(nil), st.payload...), Revision: st.revision})
	}
	sort.Slice(snap.Nodes, func(i, j int) bool { return snap.Nodes[i].Name < snap.Nodes[j].Name })
	for k, rev := range s.edges {
		snap.Edges = append(snap.Edges, Edge{From: k.from, To: k.to, Revision: rev})
	}
	sort.Slice(snap.Edges, func(i, j int) bool {
		if snap.Edges[i].From != snap.Edges[j].From {
			return snap.Edges[i].From < snap.Edges[j].From
		}
		return snap.Edges[i].To < snap.Edges[j].To
	})
	return snap
}
