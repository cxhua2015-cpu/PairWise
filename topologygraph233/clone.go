package topologygraph233

// Clone returns a fully independent deep copy, including logical clocks.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := &Graph{
		maxNodes:   g.maxNodes,
		maxEdges:   g.maxEdges,
		maxNameLen: g.maxNameLen,
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
	for from, set := range g.adj {
		ns := make(map[string]struct{}, len(set))
		for to := range set {
			ns[to] = struct{}{}
		}
		c.adj[from] = ns
	}
	return c, nil
}
