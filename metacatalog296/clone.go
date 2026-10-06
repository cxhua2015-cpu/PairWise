package metacatalog296

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make(map[string]Record, len(s.records))
	for name, rec := range s.records {
		records[name] = Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision}
	}
	return &Store{
		opts:       s.opts,
		records:    records,
		generation: s.generation,
		revision:   s.revision,
		totalValue: s.totalValue,
	}, nil
}
