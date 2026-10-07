package metacatalog426

type Stats struct {
	Generation, NextRevision uint64
	Records, TotalValueBytes int
}

// Stats returns a linearizable summary of the current state.
func (s *Store) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statsLocked()
}

func (s *Store) statsLocked() Stats {
	st := Stats{Generation: s.generation, NextRevision: s.nextRevision, Records: len(s.records)}
	for _, v := range s.records {
		st.TotalValueBytes += len(v)
	}
	return st
}
