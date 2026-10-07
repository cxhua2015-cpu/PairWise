package topologygraph418

type Stats struct {
	Generation   uint64
	Nodes, Edges int
}

// Stats returns a linearizable summary of the current state: the counts
// and generation are read under a single read lock, so they always
// describe one consistent point in the graph's history.
func (g *Graph) Stats() Stats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return Stats{Generation: g.gen, Nodes: len(g.nodes), Edges: len(g.edges)}
}
