package readyqueue440

// Preview simulates Apply from one linearizable snapshot of the receiver.
// It replays the full transaction semantics on an isolated clone taken
// under the queue lock, returning the candidate Result, Snapshot and
// Stats exactly as a real Apply committed on that same state would
// produce them. The receiver's state, generation, revision counters and
// logical clock are never modified. On failure it returns the same error
// Apply would return, with all other return values zero.
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
