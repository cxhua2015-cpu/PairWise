package topologygraph428

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (g *Graph) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := g.validateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	candidate, err := g.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	result, err := candidate.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return result, candidate.Snapshot(), candidate.Stats(), nil
}
