package metacatalog406

type Stats struct {
	Generation, NextRevision uint64
	Records, TotalValueBytes int
}

// Stats returns a linearizable summary of the current state.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := Stats{Generation: s.generation, NextRevision: s.nextRevision, Records: len(s.records)}
	for _, e := range s.records {
		st.TotalValueBytes += len(e.value)
	}
	return st
}
