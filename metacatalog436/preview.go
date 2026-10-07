package metacatalog436

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
//
// The receiver's state is cloned under a single read lock (the linearization
// point), then the full Apply transaction semantics run on the candidate.
// The candidate's Result, Snapshot and Stats are exactly what a real Apply of
// the same batch would have committed at that snapshot. On error all return
// values are zero and the error matches Apply's error and priority.
func (s *Store) Preview(b Batch) (Result, Snapshot, Stats, error) {
	candidate, err := s.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	candidate.mu.Lock()
	defer candidate.mu.Unlock()
	res, records, generation, nextRevision, err := applyInner(candidate.opts, candidate.records, candidate.generation, candidate.nextRevision, b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	candidate.records = records
	candidate.generation = generation
	candidate.nextRevision = nextRevision
	return res, candidate.snapshotLocked(), candidate.statsLocked(), nil
}
