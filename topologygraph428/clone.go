package topologygraph428

// Clone returns a fully independent deep copy, including logical clocks.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	nodes, edges := g.copyStateLocked()
	return &Graph{
		opts:       g.opts,
		nodes:      nodes,
		edges:      edges,
		generation: g.generation,
	}, nil
}
