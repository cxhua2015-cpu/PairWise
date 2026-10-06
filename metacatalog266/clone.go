package metacatalog266

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make(map[string]Record, len(s.records))
	for name, rec := range s.records {
		records[name] = cloneRecord(rec)
	}
	return &Store{
		opts:       s.opts,
		records:    records,
		generation: s.generation,
		nextRev:    s.nextRev,
	}, nil
}
