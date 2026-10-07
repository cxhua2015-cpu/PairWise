package readyqueue435

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
// It clones the current state (a single linearization point under the read lock),
// runs the full transaction semantics on the candidate, and returns the candidate
// Result, Snapshot, and Stats. On failure it returns the same error Apply would
// return on the same state, with all other return values zero.
func (q *Queue) Preview(b Batch) (Result, Snapshot, Stats, error) {
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
