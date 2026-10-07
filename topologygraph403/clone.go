package topologygraph403

// Clone returns a fully independent deep copy, including logical clocks.
// Mutating the clone never affects the original and vice versa.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := g.candidate()
	c.generation = g.generation
	return c, nil
}
