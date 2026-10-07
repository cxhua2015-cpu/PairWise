package metacatalog411

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clone := &Store{
		opts:       s.opts,
		records:    make(map[string]Record, len(s.records)),
		totalBytes: s.totalBytes,
		generation: s.generation,
		nextRev:    s.nextRev,
	}
	for name, rec := range s.records {
		value := make([]byte, len(rec.Value))
		copy(value, rec.Value)
		rec.Value = value
		clone.records[name] = rec
	}
	return clone, nil
}
