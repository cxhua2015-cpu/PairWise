package topologygraph438

// Clone returns a fully independent deep copy, including logical clocks.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := &Graph{
		opts:       g.opts,
		nodes:      make(map[string]struct{}, len(g.nodes)),
		edges:      make(map[Edge]struct{}, len(g.edges)),
		adj:        make(map[string]map[string]struct{}, len(g.adj)),
		generation: g.generation,
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, tos := range g.adj {
		m := make(map[string]struct{}, len(tos))
		for to := range tos {
			m[to] = struct{}{}
		}
		c.adj[from] = m
	}
	return c, nil
}
