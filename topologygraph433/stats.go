package topologygraph433

type Stats struct {
	Generation   uint64
	Nodes, Edges int
}

// Stats returns a linearizable summary of the current state.
func (g *Graph) Stats() Stats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.statsLocked()
}

func (g *Graph) statsLocked() Stats {
	return Stats{Generation: g.generation, Nodes: len(g.nodes), Edges: len(g.edges)}
}
