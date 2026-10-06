package metacatalog221

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &Store{
		opts:            s.opts,
		records:         make(map[string]*Record, len(s.records)),
		generation:      s.generation,
		nextRevision:    s.nextRevision,
		totalValueBytes: s.totalValueBytes,
	}
	for name, r := range s.records {
		v := make([]byte, len(r.Value))
		copy(v, r.Value)
		c.records[name] = &Record{Name: r.Name, Value: v, Revision: r.Revision}
	}
	return c, nil
}
