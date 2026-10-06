package metacatalog236

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &Store{
		opts:       s.opts,
		records:    make(map[string]Record, len(s.records)),
		generation: s.generation,
		nextRev:    s.nextRev,
		totalValue: s.totalValue,
	}
	for k, v := range s.records {
		c.records[k] = cloneRecord(v)
	}
	return c, nil
}
