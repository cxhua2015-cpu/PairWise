package topologygraph238

// Clone returns a fully independent deep copy, including logical clocks:
// the clone keeps the same options and generation, but owns private index
// maps so later mutations of either graph never alias the other.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := &Graph{
		opts:       g.opts,
		generation: g.generation,
		nodes:      make(map[string]struct{}, len(g.nodes)),
		edges:      make(map[Edge]struct{}, len(g.edges)),
		out:        make(map[string]map[string]struct{}, len(g.out)),
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
