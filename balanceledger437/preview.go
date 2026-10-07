package balanceledger437

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (l *Ledger) Preview(b Batch) (Result, Snapshot, Stats, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	candidate := state{
		accounts:     make(map[string]Account, len(l.st.accounts)),
		generation:   l.st.generation,
		nextRevision: l.st.nextRevision,
	}
	for k, v := range l.st.accounts {
		candidate.accounts[k] = v
	}
	res, err := applyBatch(l.opts, &candidate, b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return res, snapshotOf(&candidate), statsOf(&candidate), nil
}
