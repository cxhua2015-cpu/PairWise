package expirytable424

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (x *Table) Preview(b Batch) (Result, Snapshot, Stats, error) {
	x.mu.RLock()
	candidate := x.cloneLocked()
	x.mu.RUnlock()
	r, err := candidate.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return r, candidate.Snapshot(), candidate.Stats(), nil
}
