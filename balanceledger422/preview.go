package balanceledger422

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (l *Ledger) Preview(b Batch) (Result, Snapshot, Stats, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	res, work, gen, rev, err := applyBatch(l.opts, l.accounts, l.generation, l.nextRevision, b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return res, snapshotOf(work, gen, rev), statsOf(work, gen, rev), nil
}
