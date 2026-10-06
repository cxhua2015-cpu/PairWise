package topologygraph283

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the original.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := &Graph{
		opts:       g.opts,
		nodes:      make(map[string]struct{}, len(g.nodes)),
		edges:      make(map[edgeKey]struct{}, len(g.edges)),
		out:        make(map[string]map[string]struct{}, len(g.out)),
		generation: g.generation,
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, tos := range g.out {
		s := make(map[string]struct{}, len(tos))
		for to := range tos {
			s[to] = struct{}{}
		}
		c.out[from] = s
	}
	return c, nil
}
