package metacatalog431

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		recs[k] = cloneRecord(v)
	}
	return &Store{
		opts:         s.opts,
		records:      recs,
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}, nil
}
