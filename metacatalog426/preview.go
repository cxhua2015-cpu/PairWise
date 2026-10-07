package metacatalog426

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (s *Store) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	s.mu.RLock()
	candidate := s.cloneLocked()
	s.mu.RUnlock()
	if len(b.Ops) == 0 {
		return Result{}, candidate.snapshotLocked(), candidate.statsLocked(), nil
	}
	res, err := candidate.applyLocked(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return res, candidate.snapshotLocked(), candidate.statsLocked(), nil
}
