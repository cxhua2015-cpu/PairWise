package metacatalog226

// Clone returns a fully independent deep copy of the store, including the
// logical clocks (generation and next revision). Every record value is
// copied, so the clone and the original share no mutable memory and can be
// used concurrently without aliasing.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := make(map[string]record, len(s.records))
	for name, r := range s.records {
		recs[name] = record{value: cloneBytes(r.value), revision: r.revision}
	}
	return &Store{
		opts:         s.opts,
		records:      recs,
		generation:   s.generation,
		nextRevision: s.nextRevision,
		totalValue:   s.totalValue,
	}, nil
}
