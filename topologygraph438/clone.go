package topologygraph438

// Clone returns a fully independent deep copy, including logical clocks.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := &Graph{
		nodes:      make(map[string]struct{}, len(g.nodes)),
		edges:      make(map[Edge]struct{}, len(g.edges)),
		generation: g.generation,
		opts:       g.opts,
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	return c, nil
}
