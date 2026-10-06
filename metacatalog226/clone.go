package metacatalog226

// Clone returns a fully independent deep copy of the store, including the
// logical clocks (generation and next revision). The clone shares no memory
// with the original: every record value is copied, so later mutations of
// either store never alias the other.
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
		c.records[name] = cloneRecord(r)
	}
	return c, nil
}
