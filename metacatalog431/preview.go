package metacatalog431

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (s *Store) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	c, err := s.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	res, err := c.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return res, c.Snapshot(), c.Stats(), nil
}
