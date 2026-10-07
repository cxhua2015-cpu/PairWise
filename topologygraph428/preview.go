package topologygraph428

// Preview simulates Apply from one linearizable snapshot without mutating the
// receiver. It takes the same write lock Apply uses, builds a throwaway
// candidate state, and runs the identical transaction; on success the
// candidate Result, Snapshot and Stats describe what Apply would commit,
// while the receiver's state, generation and ownership stay untouched. On
// failure the error matches Apply and all value returns are zero.
func (g *Graph) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	candidate := g.state.clone()
	if err := candidate.apply(b, g.opts); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	generation := g.generation
	if len(b.Ops) > 0 {
		generation++
	}
	result := Result{Generation: generation}
	snap := candidate.snapshot(generation)
	stats := Stats{Generation: generation, Nodes: len(candidate.nodes), Edges: len(candidate.edges)}
	return result, snap, stats, nil
}
