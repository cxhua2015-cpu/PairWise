package topologygraph413

type Stats struct {
	Generation   uint64
	Nodes, Edges int
}

// Stats returns a linearizable summary of the current state: the counts
// and generation are read under the same read lock, so they always
// describe one consistent committed state.
func (g *Graph) Stats() Stats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return Stats{
		Generation: g.gen,
		Nodes:      len(g.st.nodes),
		Edges:      len(g.st.edges),
	}
}
