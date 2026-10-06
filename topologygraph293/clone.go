package topologygraph293

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the original.
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
	for k, v := range g.out {
		s := make(map[string]struct{}, len(v))
		for x := range v {
			s[x] = struct{}{}
		}
		c.out[k] = s
	}
	for k, v := range g.in {
		s := make(map[string]struct{}, len(v))
		for x := range v {
			s[x] = struct{}{}
		}
		c.in[k] = s
	}
	return c, nil
}
