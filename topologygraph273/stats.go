package topologygraph273

type Stats struct {
	Generation   uint64
	Nodes, Edges int
}

// Stats returns a linearizable summary of the current state: the read lock
// guarantees the generation and both counters come from one consistent
// committed point in time.
func (g *Graph) Stats() Stats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return Stats{Generation: g.gen, Nodes: len(g.nodes), Edges: len(g.edges)}
}
