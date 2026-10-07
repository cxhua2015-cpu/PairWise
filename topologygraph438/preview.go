package topologygraph438

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (g *Graph) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := g.ValidateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	candidate, err := g.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	res, err := candidate.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return res, candidate.Snapshot(), candidate.Stats(), nil
}
