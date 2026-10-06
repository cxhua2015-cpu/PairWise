package metacatalog291

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &Store{
		records:      make(map[string]Record, len(s.records)),
		generation:   s.generation,
		nextRevision: s.nextRevision,
		opts:         s.opts,
	}
	for k, r := range s.records {
		c.records[k] = cloneRecord(r)
	}
	return c, nil
}
