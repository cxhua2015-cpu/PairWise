package metacatalog251

// Clone returns a fully independent deep copy of the store, preserving the
// logical clocks (generation and nextRevision). All record values are
// copied, so the clone and the original share no mutable state and can be
// used concurrently without aliasing.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index := make(map[string]Record, len(s.index))
	for name, rec := range s.index {
		index[name] = cloneRecord(rec)
	}
	return &Store{
		opts:            s.opts,
		index:           index,
		generation:      s.generation,
		nextRevision:    s.nextRevision,
		totalValueBytes: s.totalValueBytes,
	}, nil
}
