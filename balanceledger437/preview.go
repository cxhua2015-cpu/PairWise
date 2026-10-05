package balanceledger437

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (l *Ledger) Preview(Batch) (Result, Snapshot, Stats, error) {
	return Result{}, Snapshot{}, Stats{}, ErrNotImplemented
}
