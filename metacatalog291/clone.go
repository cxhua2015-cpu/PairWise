package metacatalog291

// Clone returns a fully independent deep copy, including logical clocks
// (generation and nextRevision). Every record Value is copied, so the clone
// and the original share no mutable memory and diverge safely under
// concurrent use.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &Store{
		opts:         s.opts,
		records:      make(map[string]Record, len(s.records)),
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}
	for name, rec := range s.records {
		c.records[name] = cloneRecord(rec)
	}
	return c, nil
}
