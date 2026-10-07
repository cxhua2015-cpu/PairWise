package balanceledger432

// Preview simulates Apply from one linearizable snapshot without mutating
// the receiver: the batch is executed against a private candidate state
// captured under the ledger lock. On success it returns the candidate
// Result, Snapshot and Stats exactly as a real Apply on that state would
// produce; on failure it returns the same error Apply would return and all
// other values are zero. The receiver's state, generation, revision and
// logical clocks are never changed.
func (l *Ledger) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	candidate := l.state.copy()
	res, err := candidate.execute(b, l.opts)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	snap := candidate.snapshot()
	stats := Stats{
		Generation:   candidate.generation,
		NextRevision: candidate.nextRevision,
		Accounts:     len(candidate.accounts),
	}
	return res, snap, stats, nil
}
