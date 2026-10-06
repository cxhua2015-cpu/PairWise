package topologygraph268

// Clone returns a fully independent deep copy, including logical clocks.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return &Graph{
		opts:       g.opts,
		st:         g.st.clone(),
		generation: g.generation,
	}, nil
}
