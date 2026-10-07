package topologygraph423

type Stats struct {
	Generation   uint64
	Nodes, Edges int
}

// Stats returns a linearizable summary of the current state.
func (g *Graph) Stats() Stats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return Stats{Generation: g.generation, Nodes: len(g.st.nodes), Edges: len(g.st.edges)}
}
