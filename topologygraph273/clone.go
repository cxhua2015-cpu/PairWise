package topologygraph273

// Clone returns a fully independent deep copy, including the logical clock
// (generation). All maps and adjacency indexes are rebuilt so the clone
// shares no ownership with the source; mutating either graph never affects
// the other.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := &Graph{
		opts:  g.opts,
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
		in:    make(map[string]map[string]struct{}, len(g.in)),
		gen:   g.gen,
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, tos := range g.out {
		nt := make(map[string]struct{}, len(tos))
		for to := range tos {
			nt[to] = struct{}{}
		}
		c.out[from] = nt
	}
	for to, froms := range g.in {
		nf := make(map[string]struct{}, len(froms))
		for from := range froms {
			nf[from] = struct{}{}
		}
		c.in[to] = nf
	}
	return c, nil
}
