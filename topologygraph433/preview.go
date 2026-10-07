package topologygraph433

// Preview simulates Apply from one linearizable snapshot without mutating the
// receiver. It clones the current state (preserving the logical clock),
// executes the full transaction semantics on the candidate, and returns the
// candidate Result, Snapshot and Stats. On failure it returns the same error
// Apply would return on this state, with all values zero.
func (g *Graph) Preview(b Batch) (Result, Snapshot, Stats, error) {
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
