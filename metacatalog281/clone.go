package metacatalog281

// Clone returns a fully independent deep copy, including the logical
// clocks (generation and nextRevision). Every record value is copied, so
// the clone and the original share no mutable memory: batches applied to
// one never affect the other, and values handed out by either store remain
// owned by that store alone.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &Store{
		opts:         s.opts,
		records:      make(map[string]Record, len(s.records)),
		generation:   s.generation,
		nextRevision: s.nextRevision,
		totalBytes:   s.totalBytes,
	}
	for n, r := range s.records {
		c.records[n] = Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision}
	}
	return c, nil
}
