package readyqueue435

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (q *Queue) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := q.validateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	candidate, err := q.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	res, err := candidate.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return res, candidate.Snapshot(), candidate.Stats(), nil
}
