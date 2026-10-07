package expirytable434

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (t *Table) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := validateBatch(t.opts, b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < t.now {
		return Result{}, Snapshot{}, Stats{}, ErrTime
	}
	cand := t.snapshotState()
	res, err := cand.apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	if len(cand.entries) > t.opts.MaxEntries {
		return Result{}, Snapshot{}, Stats{}, ErrCapacity
	}
	if len(b.Ops) > 0 {
		cand.generation++
	}
	cand.now = b.Now
	res.Generation = cand.generation
	if res.Revision == 0 {
		res.Revision = cand.nextRevision - 1
	}
	snap := Snapshot{
		Generation:   cand.generation,
		NextRevision: cand.nextRevision,
		Now:          cand.now,
		Entries:      sortedEntries(cand.entries),
	}
	stats := Stats{
		Generation:   cand.generation,
		NextRevision: cand.nextRevision,
		Now:          cand.now,
		Entries:      len(cand.entries),
	}
	return res, snap, stats, nil
}
