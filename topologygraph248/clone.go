package topologygraph248

// Clone returns a fully independent deep copy, including logical clocks.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := g.candidate()
	return &Graph{
		opts:  g.opts,
		gen:   g.gen,
		nodes: c.nodes,
		edges: c.edges,
		out:   c.out,
		in:    c.in,
	}, nil
}
