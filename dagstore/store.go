package dagstore

import (
	"sort"
)

type nodeState struct {
	payload  []byte
	revision uint64
}

type edgeKey struct{ from, to string }

func validName(opts Options, n string) bool {
	if n == "" || len(n) > opts.MaxNameBytes {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

func New(opts Options) (*Store, error) {
	if opts.MaxNodes <= 0 || opts.MaxEdges <= 0 || opts.MaxNameBytes <= 0 ||
		opts.MaxPayloadBytes <= 0 || opts.MaxTotalPayloadBytes <= 0 ||
		opts.MaxPayloadBytes > opts.MaxTotalPayloadBytes {
		return nil, ErrInvalidOptions
	}
	return &Store{
		opts:  opts,
		nodes: make(map[string]*nodeState),
		edges: make(map[edgeKey]uint64),
		out:   make(map[string]map[string]struct{}),
		in:    make(map[string]map[string]struct{}),
	}, nil
}

func validateOp(opts Options, op Op) error {
	switch op.Kind {
	case AddNode, UpdateNode:
		if !validName(opts, op.Name) || op.Payload == nil || len(op.Payload) > opts.MaxPayloadBytes {
			return ErrInvalidInput
		}
	case DeleteNode:
		if !validName(opts, op.Name) || op.Payload != nil {
			return ErrInvalidInput
		}
	case AddEdge, RemoveEdge:
		if op.Name != "" || op.Payload != nil || op.From == op.To ||
			!validName(opts, op.From) || !validName(opts, op.To) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// candidate is an isolated mutable copy of the committed state.
type candidate struct {
	nodes    map[string]*nodeState
	edges    map[edgeKey]uint64
	out      map[string]map[string]struct{}
	in       map[string]map[string]struct{}
	revision uint64
}

func (s *Store) fork() *candidate {
	c := &candidate{
		nodes:    make(map[string]*nodeState, len(s.nodes)),
		edges:    make(map[edgeKey]uint64, len(s.edges)),
		out:      make(map[string]map[string]struct{}, len(s.out)),
		in:       make(map[string]map[string]struct{}, len(s.in)),
		revision: s.revision,
	}
	for n, st := range s.nodes {
		c.nodes[n] = &nodeState{payload: st.payload, revision: st.revision}
	}
	for k, r := range s.edges {
		c.edges[k] = r
	}
	for k, m := range s.out {
		cm := make(map[string]struct{}, len(m))
		for v := range m {
			cm[v] = struct{}{}
		}
		c.out[k] = cm
	}
	for k, m := range s.in {
		cm := make(map[string]struct{}, len(m))
		for v := range m {
			cm[v] = struct{}{}
		}
		c.in[k] = cm
	}
	return c
}

func (c *candidate) addEdge(from, to string) {
	if c.out[from] == nil {
		c.out[from] = make(map[string]struct{})
	}
	c.out[from][to] = struct{}{}
	if c.in[to] == nil {
		c.in[to] = make(map[string]struct{})
	}
	c.in[to][from] = struct{}{}
}

func (c *candidate) removeEdge(from, to string) {
	delete(c.out[from], to)
	if len(c.out[from]) == 0 {
		delete(c.out, from)
	}
	delete(c.in[to], from)
	if len(c.in[to]) == 0 {
		delete(c.in, to)
	}
}

// reaches reports whether target is reachable from start via out-edges.
func (c *candidate) reaches(start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]struct{}{start: {}}
	stack := []string{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range c.out[n] {
			if next == target {
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

func (s *Store) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := validateOp(s.opts, op); err != nil {
			return Result{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	c := s.fork()
	changedNodes := make(map[string]Node)
	changedEdges := make(map[edgeKey]Edge)

	for _, op := range b.Ops {
		c.revision++
		rev := c.revision
		switch op.Kind {
		case AddNode:
			if _, ok := c.nodes[op.Name]; ok {
				return Result{}, ErrConflict
			}
			p := make([]byte, len(op.Payload))
			copy(p, op.Payload)
			c.nodes[op.Name] = &nodeState{payload: p, revision: rev}
			changedNodes[op.Name] = Node{Name: op.Name, Payload: p, Revision: rev}
		case UpdateNode:
			st, ok := c.nodes[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			p := make([]byte, len(op.Payload))
			copy(p, op.Payload)
			st.payload = p
			st.revision = rev
			changedNodes[op.Name] = Node{Name: op.Name, Payload: p, Revision: rev}
		case DeleteNode:
			if _, ok := c.nodes[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			if len(c.out[op.Name]) > 0 || len(c.in[op.Name]) > 0 {
				return Result{}, ErrConflict
			}
			delete(c.nodes, op.Name)
			delete(changedNodes, op.Name)
		case AddEdge:
			if _, ok := c.nodes[op.From]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := c.nodes[op.To]; !ok {
				return Result{}, ErrNotFound
			}
			k := edgeKey{op.From, op.To}
			if _, ok := c.edges[k]; ok {
				return Result{}, ErrConflict
			}
			if c.reaches(op.To, op.From) {
				return Result{}, ErrCycle
			}
			c.edges[k] = rev
			c.addEdge(op.From, op.To)
			changedEdges[k] = Edge{From: op.From, To: op.To, Revision: rev}
		case RemoveEdge:
			k := edgeKey{op.From, op.To}
			if _, ok := c.edges[k]; !ok {
				return Result{}, ErrNotFound
			}
			delete(c.edges, k)
			c.removeEdge(op.From, op.To)
			delete(changedEdges, k)
		}
	}

	if len(c.nodes) > s.opts.MaxNodes || len(c.edges) > s.opts.MaxEdges {
		return Result{}, ErrCapacity
	}
	total := 0
	for _, st := range c.nodes {
		total += len(st.payload)
	}
	if total > s.opts.MaxTotalPayloadBytes {
		return Result{}, ErrCapacity
	}

	s.nodes = c.nodes
	s.edges = c.edges
	s.out = c.out
	s.in = c.in
	s.revision = c.revision
	if len(b.Ops) > 0 {
		s.generation++
	}

	res := Result{Generation: s.generation, Revision: s.revision}
	names := make([]string, 0, len(changedNodes))
	for n := range changedNodes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		cn := changedNodes[n]
		p := make([]byte, len(cn.Payload))
		copy(p, cn.Payload)
		cn.Payload = p
		res.ChangedNodes = append(res.ChangedNodes, cn)
	}
	keys := make([]edgeKey, 0, len(changedEdges))
	for k := range changedEdges {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].from != keys[j].from {
			return keys[i].from < keys[j].from
		}
		return keys[i].to < keys[j].to
	})
	for _, k := range keys {
		res.ChangedEdges = append(res.ChangedEdges, changedEdges[k])
	}
	return res, nil
}

func (s *Store) Reachable(from, to string) (bool, error) {
	if !validName(s.opts, from) || !validName(s.opts, to) {
		return false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.nodes[from]; !ok {
		return false, ErrNotFound
	}
	if _, ok := s.nodes[to]; !ok {
		return false, ErrNotFound
	}
	c := &candidate{out: s.out}
	return c.reaches(from, to), nil
}

func (s *Store) Topological() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	indeg := make(map[string]int, len(s.nodes))
	for n := range s.nodes {
		indeg[n] = 0
	}
	for to, m := range s.in {
		indeg[to] = len(m)
	}
	ready := make([]string, 0, len(s.nodes))
	for n, d := range indeg {
		if d == 0 {
			ready = append(ready, n)
		}
	}
	sort.Strings(ready)
	order := make([]string, 0, len(s.nodes))
	for len(ready) > 0 {
		n := ready[0]
		ready = ready[1:]
		order = append(order, n)
		for next := range s.out[n] {
			indeg[next]--
			if indeg[next] == 0 {
				i := sort.SearchStrings(ready, next)
				ready = append(ready, "")
				copy(ready[i+1:], ready[i:])
				ready[i] = next
			}
		}
	}
	return order
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{Generation: s.generation, NextRevision: s.revision + 1}
	names := make([]string, 0, len(s.nodes))
	for n := range s.nodes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		st := s.nodes[n]
		p := make([]byte, len(st.payload))
		copy(p, st.payload)
		snap.Nodes = append(snap.Nodes, Node{Name: n, Payload: p, Revision: st.revision})
	}
	keys := make([]edgeKey, 0, len(s.edges))
	for k := range s.edges {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].from != keys[j].from {
			return keys[i].from < keys[j].from
		}
		return keys[i].to < keys[j].to
	})
	for _, k := range keys {
		snap.Edges = append(snap.Edges, Edge{From: k.from, To: k.to, Revision: s.edges[k]})
	}
	return snap
}
