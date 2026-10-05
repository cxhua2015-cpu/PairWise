package metacatalog421

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (s *Store) Preview(Batch) (Result, Snapshot, Stats, error) {
	return Result{}, Snapshot{}, Stats{}, ErrNotImplemented
}
