package metacatalog256

type Stats struct {
	Generation, NextRevision uint64
	Records, TotalValueBytes int
}

// Stats returns a linearizable summary of the current state: it is
// computed under the read lock, so it always reflects a single
// consistent point between committed transactions.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := Stats{
		Generation:   s.generation,
		NextRevision: s.nextRevision,
		Records:      len(s.records),
	}
	for _, rec := range s.records {
		stats.TotalValueBytes += len(rec.Value)
	}
	return stats
}
