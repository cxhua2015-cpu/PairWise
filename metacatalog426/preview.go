package metacatalog426

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (s *Store) Preview(b Batch) (Result, Snapshot, Stats, error) {
	candidate, err := s.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	res, err := candidate.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return res, candidate.Snapshot(), candidate.Stats(), nil
}
