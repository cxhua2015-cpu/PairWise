package expirytable439

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (x *Table) Preview(b Batch) (Result, Snapshot, Stats, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	candidate := x.cloneLocked()
	result, err := candidate.applyBatch(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return result, candidate.snapshotLocked(), candidate.statsLocked(), nil
}
