package metacatalog416

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make(map[string]Record, len(s.records))
	for k, r := range s.records {
		records[k] = Record{Name: r.Name, Value: cloneBytes(r.Value), Revision: r.Revision}
	}
	return &Store{
		opts:         s.opts,
		records:      records,
		totalValue:   s.totalValue,
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}, nil
}
