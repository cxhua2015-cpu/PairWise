package topologygraph423

// Clone returns a fully independent deep copy, including logical clocks.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return &Graph{st: g.st.copy(), generation: g.generation, opts: g.opts}, nil
}
