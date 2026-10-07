package balanceledger427

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (l *Ledger) Preview(b Batch) (Result, Snapshot, Stats, error) {
	l.mu.RLock()
	candidate := l.cloneLocked()
	l.mu.RUnlock()

	result, err := candidate.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return result, candidate.Snapshot(), candidate.Stats(), nil
}
