package metacatalog431

type Stats struct {
	Generation, NextRevision uint64
	Records, TotalValueBytes int
}

// stats summarizes st; caller must hold the lock or own the state.
func (st *state) stats() Stats {
	return Stats{
		Generation:      st.generation,
		NextRevision:    st.nextRevision,
		Records:         len(st.records),
		TotalValueBytes: st.totalValueBytes,
	}
}

// Stats returns a linearizable summary of the current state.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.stats()
}
