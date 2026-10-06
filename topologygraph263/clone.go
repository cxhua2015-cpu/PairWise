package topologygraph263

// Clone returns a fully independent deep copy of the graph, including
// the logical clock (generation). The clone owns all of its maps and
// slices; subsequent mutations of either graph never alias the other.
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
	for from, set := range g.adj {
		s := make(map[string]struct{}, len(set))
		for to := range set {
			s[to] = struct{}{}
		}
		c.adj[from] = s
	}
	return c, nil
}
