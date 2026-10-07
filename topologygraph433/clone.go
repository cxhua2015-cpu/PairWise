package topologygraph433

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no ownership with the original: all indexes are copied.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	nodes, edges := g.copyIndexes()
	return &Graph{
		opts:       g.opts,
		nodes:      nodes,
		edges:      edges,
		generation: g.generation,
	}, nil
}
