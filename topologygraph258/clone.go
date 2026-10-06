package topologygraph258

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the original.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := &Graph{
		maxNodes:   g.maxNodes,
		maxEdges:   g.maxEdges,
		nameBytes:  g.nameBytes,
		generation: g.generation,
		nodes:      make(map[string]struct{}, len(g.nodes)),
		edges:      make(map[Edge]struct{}, len(g.edges)),
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	return c, nil
}
