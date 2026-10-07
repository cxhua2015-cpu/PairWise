package metacatalog421

type Stats struct {
	Generation, NextRevision uint64
	Records, TotalValueBytes int
}

// Stats returns a linearizable summary of the current state.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statsLocked()
}

func (s *Store) statsLocked() Stats {
	return Stats{
		Generation:      s.generation,
		NextRevision:    s.nextRev,
		Records:         len(s.records),
		TotalValueBytes: s.totalBytes,
	}
}
