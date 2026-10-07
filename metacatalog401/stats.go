package metacatalog401

type Stats struct {
	Generation, NextRevision uint64
	Records, TotalValueBytes int
}

// Stats returns a linearizable summary of the current state: it observes a
// single committed generation under the read lock, so concurrent Apply calls
// are either fully reflected or not reflected at all.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Stats{
		Generation:      s.generation,
		NextRevision:    s.nextRevision,
		Records:         len(s.records),
		TotalValueBytes: s.totalValueBytes,
	}
}
