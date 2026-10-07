package metacatalog401

// Clone returns a fully independent deep copy, including logical clocks
// (generation and next revision). Every record value is copied, so the clone
// and the original share no mutable memory and can be used concurrently.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make(map[string]Record, len(s.records))
	for k, rec := range s.records {
		records[k] = Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision}
	}
	return &Store{
		opts:            s.opts,
		records:         records,
		generation:      s.generation,
		nextRevision:    s.nextRevision,
		totalValueBytes: s.totalValueBytes,
	}, nil
}
