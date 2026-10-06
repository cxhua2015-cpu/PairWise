package topologygraph263

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the original.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := g.candidate()
	c.generation = g.generation
	return c, nil
}
