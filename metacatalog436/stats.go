package metacatalog436

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
	total := 0
	for _, rec := range s.records {
		total += len(rec.Value)
	}
	return Stats{
		Generation:      s.generation,
		NextRevision:    s.nextRevision,
		Records:         len(s.records),
		TotalValueBytes: total,
	}
}
