package expirytable424

// Stats is a linearizable summary of table state.
type Stats struct {
	Generation, NextRevision uint64
	Now                      int64
	Entries                  int
}

// Stats returns a linearizable summary of the current state.
func (t *Table) Stats() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.st.stats()
}

func (s state) stats() Stats {
	return Stats{
		Generation:   s.generation,
		NextRevision: s.nextRevision,
		Now:          s.now,
		Entries:      len(s.entries),
	}
}
