package metacatalog276

type Stats struct {
	Generation, NextRevision uint64
	Records, TotalValueBytes int
}

// Stats returns a linearizable summary of the current state.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, e := range s.records {
		total += len(e.value)
	}
	return Stats{
		Generation:      s.generation,
		NextRevision:    s.nextRevision,
		Records:         len(s.records),
		TotalValueBytes: total,
	}
}
