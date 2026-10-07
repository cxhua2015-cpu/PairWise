package metacatalog406

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
	for k, v := range s.records {
		c.records[k] = entry{value: cloneBytes(v.value), revision: v.revision}
	}
	return c, nil
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
