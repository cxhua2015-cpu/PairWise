package topologygraph433

// Clone returns a fully independent deep copy, including logical clocks.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := g.lockedCloneState()
	c.generation = g.generation
	return c, nil
}
