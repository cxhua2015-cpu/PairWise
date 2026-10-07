package metacatalog421

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cloneLocked(), nil
}

func (s *Store) cloneLocked() *Store {
	c := &Store{
		opts:       s.opts,
		records:    make(map[string]Record, len(s.records)),
		totalBytes: s.totalBytes,
		generation: s.generation,
		nextRev:    s.nextRev,
	}
	for name, rec := range s.records {
		c.records[name] = Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision}
	}
	return c
}
