package topologygraph248

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the original.
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
	for from, set := range g.out {
		ns := make(map[string]struct{}, len(set))
		for to := range set {
			ns[to] = struct{}{}
		}
		c.out[from] = ns
	}
	for to, set := range g.in {
		ns := make(map[string]struct{}, len(set))
		for from := range set {
			ns[from] = struct{}{}
		}
		c.in[to] = ns
	}
	return c, nil
}
