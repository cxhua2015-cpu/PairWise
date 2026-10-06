package topologygraph233

// Clone returns a fully independent deep copy, including logical clocks.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := &Graph{
		opts:       g.opts,
		nodes:      make(map[string]struct{}, len(g.nodes)),
		edges:      make(map[Edge]struct{}, len(g.edges)),
		out:        make(map[string]map[string]struct{}, len(g.out)),
		in:         make(map[string]map[string]struct{}, len(g.in)),
		generation: g.generation,
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, m := range g.out {
		cm := make(map[string]struct{}, len(m))
		for to := range m {
			cm[to] = struct{}{}
		}
		c.out[from] = cm
	}
	for to, m := range g.in {
		cm := make(map[string]struct{}, len(m))
		for from := range m {
			cm[from] = struct{}{}
		}
		c.in[to] = cm
	}
	return c, nil
}
