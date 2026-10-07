package topologygraph433

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (g *Graph) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	w := g.lockedCloneState()
	w.generation = g.generation
	if len(b.Ops) == 0 {
		return Result{Generation: g.generation}, g.snapshotLocked(), g.statsLocked(), nil
	}
	if err := w.applyOps(b.Ops); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	if len(w.nodes) > g.opts.MaxNodes || len(w.edges) > g.opts.MaxEdges {
		return Result{}, Snapshot{}, Stats{}, ErrCapacity
	}
	w.generation++
	return Result{Generation: w.generation}, w.snapshotLocked(), w.statsLocked(), nil
}
