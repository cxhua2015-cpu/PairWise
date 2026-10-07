package topologygraph413

// Clone returns a fully independent deep copy of the graph, including its
// logical clock (generation). The clone shares no maps with the original, so
// subsequent transactions on either graph never alias each other.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	clone := &Graph{opts: g.opts, generation: g.generation}
	c := g.candidate()
	clone.nodes = c.nodes
	clone.edges = c.edges
	clone.out = c.out
	clone.in = c.in
	return clone, nil
}
