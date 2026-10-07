package balanceledger437

// Preview simulates Apply against one linearizable snapshot of the ledger
// without mutating the receiver, its generation, revisions, or logical
// clocks. On success it returns the candidate Result, Snapshot, and Stats
// exactly as a real Apply of the same batch on the same state would produce.
// On failure it returns the same error Apply would return and zero values.
func (l *Ledger) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
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
