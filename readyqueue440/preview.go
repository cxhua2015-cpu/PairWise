package readyqueue440

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (q *Queue) Preview(Batch) (Result, Snapshot, Stats, error) {
	return Result{}, Snapshot{}, Stats{}, ErrNotImplemented
}
