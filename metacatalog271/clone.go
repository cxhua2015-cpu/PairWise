package metacatalog271

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &Store{
		opts:         s.opts,
		records:      make(map[string]entry, len(s.records)),
		totalValue:   s.totalValue,
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}
	for k, v := range s.records {
		c.records[k] = entry{value: append([]byte(nil), v.value...), revision: v.revision}
	}
	return c, nil
}
