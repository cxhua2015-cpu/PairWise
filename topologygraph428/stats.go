package topologygraph428

// Stats is a linearizable summary of one committed graph state.
type Stats struct {
	Generation uint64
	Nodes      int
	Edges      int
}

// Stats returns a linearizable summary of the current state. The counts and
// generation are read together under the read lock, so they always describe
// one committed snapshot.
func (g *Graph) Stats() Stats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return Stats{Generation: g.generation, Nodes: len(g.state.nodes), Edges: len(g.state.edges)}
}
