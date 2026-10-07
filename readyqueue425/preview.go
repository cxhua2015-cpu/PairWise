package readyqueue425

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
// It clones the current state, runs the full transaction semantics on the
// clone, and reports the candidate Result, Snapshot and Stats. The
// receiver's state, generation, revision and logical clock are unchanged.
// On failure it returns the same error Apply would return on the same
// state, and all other return values are zero.
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
