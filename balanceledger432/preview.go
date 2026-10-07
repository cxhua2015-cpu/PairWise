package balanceledger432

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (l *Ledger) Preview(b Batch) (Result, Snapshot, Stats, error) {
	candidate, err := l.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	res, err := candidate.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	candidate.mu.RLock()
	defer candidate.mu.RUnlock()
	return res, candidate.snapshotLocked(), candidate.statsLocked(), nil
}
