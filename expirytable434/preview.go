package expirytable434

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
// It clones the receiver under the lock (one consistent snapshot), then
// runs the exact Apply transaction on the clone, so the returned Result,
// candidate Snapshot and candidate Stats match a real commit of the same
// batch on the same state, including error values and priority. The
// receiver's state, generation, revisions and logical clock are untouched;
// on failure every return value is the zero value alongside the error.
func (x *Table) Preview(b Batch) (Result, Snapshot, Stats, error) {
	candidate, err := x.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	result, err := candidate.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return result, candidate.Snapshot(), candidate.Stats(), nil
}
