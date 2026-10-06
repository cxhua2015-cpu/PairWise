package metacatalog261

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make(map[string]Record, len(s.records))
	for name, rec := range s.records {
		rec.Value = append([]byte(nil), rec.Value...)
		records[name] = rec
	}
	return &Store{
		opts:       s.opts,
		records:    records,
		generation: s.generation,
		revision:   s.revision,
		totalValue: s.totalValue,
	}, nil
}
