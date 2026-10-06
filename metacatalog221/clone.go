package metacatalog221

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make(map[string]entry, len(s.records))
	for k, v := range s.records {
		value := make([]byte, len(v.value))
		copy(value, v.value)
		records[k] = entry{value: value, revision: v.revision}
	}
	return &Store{
		opts:         s.opts,
		records:      records,
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}, nil
}
