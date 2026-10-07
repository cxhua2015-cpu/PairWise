package expirytable429

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
// It returns the candidate Result, Snapshot, and Stats exactly as a real
// Apply on the same state would produce them. On failure all return values
// are zero and the error matches Apply's error and priority. The receiver's
// state, generation, revisions, and logical clock are never changed.
func (t *Table) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := t.validateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	t.mu.RLock()
	candidate := tableState{
		now:          t.state.now,
		generation:   t.state.generation,
		nextRevision: t.state.nextRevision,
		entries:      make(map[string]Entry, len(t.state.entries)),
	}
	for k, e := range t.state.entries {
		candidate.entries[k] = e
	}
	t.mu.RUnlock()
	res, err := candidate.apply(b, t.opts)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	snap := candidate.snapshot()
	stats := Stats{
		Generation:   candidate.generation,
		NextRevision: candidate.nextRevision,
		Now:          candidate.now,
		Entries:      len(candidate.entries),
	}
	return res, snap, stats, nil
}
