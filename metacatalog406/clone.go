package metacatalog406

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &Store{
		opts:    s.opts,
		records: make(map[string]Record, len(s.records)),
		total:   s.total,
		gen:     s.gen,
		nextRev: s.nextRev,
	}
	for k, r := range s.records {
		c.records[k] = Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision}
	}
	return c, nil
}
