package readyqueue425

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (q *Queue) Preview(b Batch) (Result, Snapshot, Stats, error) {
	candidate, err := q.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	result, err := candidate.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	candidate.mu.Lock()
	snapshot := candidate.snapshotLocked()
	stats := candidate.statsLocked()
	candidate.mu.Unlock()
	return result, snapshot, stats, nil
}
