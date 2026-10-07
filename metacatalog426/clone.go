package metacatalog426

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := &Store{
		opts:         s.opts,
		records:      make(map[string][]byte, len(s.records)),
		revision:     make(map[string]uint64, len(s.revision)),
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}
	for k, v := range s.records {
		c.records[k] = append([]byte(nil), v...)
	}
	for k, v := range s.revision {
		c.revision[k] = v
	}
	return c, nil
}
