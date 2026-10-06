package metacatalog246

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make(map[string]Record, len(s.records))
	for k, rec := range s.records {
		records[k] = Record{Name: rec.Name, Value: cloneBytes(rec.Value), Revision: rec.Revision}
	}
	return &Store{
		opts:       s.opts,
		records:    records,
		generation: s.generation,
		revision:   s.revision,
	}, nil
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
