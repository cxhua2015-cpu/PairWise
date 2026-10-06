package topologygraph278

// Clone returns a fully independent deep copy of the graph, including its
// logical clock (generation). The clone shares no mutable memory with the
// original: every map and nested adjacency set is freshly allocated, so
// subsequent batches on either graph can never alias the other.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := &Graph{
		opts:  g.opts,
		gen:   g.gen,
		nodes: make(map[string]struct{}, len(g.nodes)),
		edges: make(map[Edge]struct{}, len(g.edges)),
		adj:   make(map[string]map[string]struct{}, len(g.adj)),
		rev:   make(map[string]map[string]struct{}, len(g.rev)),
	}
	for n := range g.nodes {
		c.nodes[n] = struct{}{}
	}
	for e := range g.edges {
		c.edges[e] = struct{}{}
	}
	for from, tos := range g.adj {
		s := make(map[string]struct{}, len(tos))
		for to := range tos {
			s[to] = struct{}{}
		}
		c.adj[from] = s
	}
	for to, froms := range g.rev {
		s := make(map[string]struct{}, len(froms))
		for from := range froms {
			s[from] = struct{}{}
		}
		c.rev[to] = s
	}
	return c, nil
}
