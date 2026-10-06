package metacatalog266

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &Store{
		opts:         s.opts,
		records:      make(map[string]entry, len(s.records)),
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}
	for n, e := range s.records {
		v := make([]byte, len(e.value))
		copy(v, e.value)
		c.records[n] = entry{value: v, revision: e.revision}
	}
	return c, nil
}
