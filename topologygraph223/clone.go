package topologygraph223

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the source graph.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := &Graph{
		maxNodes:   g.maxNodes,
		maxEdges:   g.maxEdges,
		maxName:    g.maxName,
		generation: g.generation,
	}
	cand := g.newCandidate()
	c.nodes, c.edges, c.out, c.in = cand.nodes, cand.edges, cand.out, cand.in
	return c, nil
}
