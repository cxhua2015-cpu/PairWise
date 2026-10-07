package topologygraph413

type Stats struct {
	Generation   uint64
	Nodes, Edges int
}

// Stats returns a linearizable summary of the current state: the read is
// taken under the graph lock, so it reflects a single consistent point in
// the transaction history.
func (g *Graph) Stats() Stats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return Stats{
		Generation: g.generation,
		Nodes:      len(g.nodes),
		Edges:      len(g.edges),
	}
}
