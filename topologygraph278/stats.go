package topologygraph278

type Stats struct {
	Generation   uint64
	Nodes, Edges int
}

// Stats returns a linearizable summary of the current state: the counts and
// generation are read under the same lock hold, so they always describe one
// consistent committed generation.
func (g *Graph) Stats() Stats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return Stats{
		Generation: g.gen,
		Nodes:      len(g.nodes),
		Edges:      len(g.edges),
	}
}
