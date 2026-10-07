package balanceledger422

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
// On success it returns the candidate Result, Snapshot and Stats exactly as a real
// commit of the same batch on the same state would produce. On failure it returns
// the same error Apply would return and all zero values.
func (l *Ledger) Preview(b Batch) (Result, Snapshot, Stats, error) {
	candidate, err := l.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	res, err := candidate.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return res, candidate.Snapshot(), candidate.Stats(), nil
}
