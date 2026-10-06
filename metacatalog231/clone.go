package metacatalog231

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make(map[string]Record, len(s.records))
	for name, rec := range s.records {
		records[name] = Record{Name: rec.Name, Value: cloneBytes(rec.Value), Revision: rec.Revision}
	}
	return &Store{
		opts:         s.opts,
		records:      records,
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}, nil
}
