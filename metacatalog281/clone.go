package metacatalog281

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &Store{
		opts:         s.opts,
		records:      make(map[string]Record, len(s.records)),
		totalValue:   s.totalValue,
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}
	for name, r := range s.records {
		c.records[name] = Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision}
	}
	return c, nil
}
