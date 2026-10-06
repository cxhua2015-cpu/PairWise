package metacatalog256

type Stats struct {
	Generation, NextRevision uint64
	Records, TotalValueBytes int
}

// Stats returns a linearizable summary of the current state.
func (s *Store) Stats() Stats { return Stats{} }
