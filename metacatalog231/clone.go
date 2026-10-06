package metacatalog231

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &Store{
		opts:         s.opts,
		records:      make(map[string]Record, len(s.records)),
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}
	for name, rec := range s.records {
		c.records[name] = Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision}
	}
	return c, nil
}
