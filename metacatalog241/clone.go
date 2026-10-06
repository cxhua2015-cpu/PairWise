package metacatalog241

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make(map[string]entry, len(s.records))
	for name, e := range s.records {
		v := make([]byte, len(e.value))
		copy(v, e.value)
		records[name] = entry{value: v, revision: e.revision}
	}
	return &Store{
		opts:         s.opts,
		records:      records,
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}, nil
}
