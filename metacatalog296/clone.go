package metacatalog296

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &Store{
		opts:         s.opts,
		records:      make(map[string][]byte, len(s.records)),
		revisions:    make(map[string]uint64, len(s.revisions)),
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}
	for k, v := range s.records {
		nv := make([]byte, len(v))
		copy(nv, v)
		c.records[k] = nv
	}
	for k, v := range s.revisions {
		c.revisions[k] = v
	}
	return c, nil
}
