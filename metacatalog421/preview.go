package metacatalog421

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (s *Store) Preview(b Batch) (Result, Snapshot, Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := s.cloneLocked()
	result, err := candidate.applyLocked(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return result, candidate.snapshotLocked(), candidate.statsLocked(), nil
}
