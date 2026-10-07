package topologygraph423

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (g *Graph) Preview(b Batch) (Result, Snapshot, Stats, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	cand, err := g.apply(g.st, b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	gen := g.generation
	if len(b.Ops) > 0 {
		gen++
	}
	snap := snapshotOf(cand, gen)
	stats := Stats{Generation: gen, Nodes: len(cand.nodes), Edges: len(cand.edges)}
	return Result{Generation: gen}, snap, stats, nil
}
