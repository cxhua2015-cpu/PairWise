package metacatalog431

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (s *Store) Preview(b Batch) (Result, Snapshot, Stats, error) {
	s.mu.RLock()
	cand := s.state.cloneState()
	opts := s.opts
	s.mu.RUnlock()

	res, err := cand.applyBatch(opts, b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return res, cand.snapshot(), cand.stats(), nil
}
