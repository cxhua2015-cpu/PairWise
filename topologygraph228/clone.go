package topologygraph228

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the original: every index map is
// rebuilt, so ownership of all returned memory is exclusive to the clone.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := &Graph{
		opts:  g.opts,
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		out:   make(map[string]map[string]struct{}, len(g.out)),
		gen:   g.gen,
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, tos := range g.out {
		m := make(map[string]struct{}, len(tos))
		for to := range tos {
			m[to] = struct{}{}
		}
		c.out[from] = m
	}
	return c, nil
}
