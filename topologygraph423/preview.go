package topologygraph423

// Preview simulates Apply from one linearizable snapshot without mutating the
// receiver. On success it returns the candidate Result, Snapshot and Stats
// exactly as a real commit of the batch would produce them. On failure it
// returns the same error Apply would return and all other values zero.
func (g *Graph) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := g.validateBatch(b); err != nil {
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
