package topologygraph288

// Clone returns a fully independent deep copy, including logical clocks.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := g.candidateLocked()
	c.gen = g.gen
	return c, nil
}
