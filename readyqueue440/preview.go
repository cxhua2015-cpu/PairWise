package readyqueue440

// Preview simulates Apply from one linearizable snapshot without mutating the
// receiver. On success it returns the candidate Result, Snapshot and Stats
// exactly as a real Apply on the same state would produce. On failure it
// returns the same error Apply would return and all zero values.
func (q *Queue) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := q.ValidateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	q.mu.Lock()
	candidate := q.cloneLocked()
	q.mu.Unlock()
	result, err := candidate.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return result, candidate.Snapshot(), candidate.Stats(), nil
}
