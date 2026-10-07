package readyqueue430

// Preview simulates Apply from one linearizable snapshot without
// mutating the receiver. It clones the queue (a single locked,
// consistent copy of items and logical clocks) and runs the real
// Apply transaction on that candidate, so the returned Result, Snapshot
// and Stats are exactly what committing the batch on the same state
// would produce. On failure all return values are zero and the error
// matches Apply's error and priority.
func (q *Queue) Preview(b Batch) (Result, Snapshot, Stats, error) {
	candidate, err := q.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	result, err := candidate.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return result, candidate.Snapshot(), candidate.Stats(), nil
}
