package metacatalog241

// Clone returns a fully independent deep copy, including logical clocks
// (generation and nextRevision). All record values are copied, so the clone
// and the original share no mutable memory and can be used concurrently.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make(map[string]Record, len(s.records))
	for name, rec := range s.records {
		records[name] = cloneRecord(rec)
	}
	return &Store{
		opts:         s.opts,
		records:      records,
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}, nil
}
