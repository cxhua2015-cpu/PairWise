package metacatalog276

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no memory with the original, so subsequent mutations of
// either store (or of values previously handed out) cannot alias each other.
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
		totalBytes:   s.totalBytes,
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}, nil
}
