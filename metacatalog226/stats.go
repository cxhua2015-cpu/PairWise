package metacatalog226

type Stats struct {
	Generation, NextRevision uint64
	Records, TotalValueBytes int
}

// Stats returns a linearizable summary of the current state: it is computed
// under the read lock, so it always reflects a single consistent point in
// the transaction history.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Stats{
		Generation:      s.generation,
		NextRevision:    s.nextRevision,
		Records:         len(s.records),
		TotalValueBytes: s.totalValue,
	}
}
