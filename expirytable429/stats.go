package expirytable429

type Stats struct {
	Generation, NextRevision uint64
	Now                      int64
	Entries                  int
}

// Stats returns a linearizable summary of the current state.
func (t *Table) Stats() Stats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return Stats{
		Generation:   t.state.generation,
		NextRevision: t.state.nextRevision,
		Now:          t.state.now,
		Entries:      len(t.state.entries),
	}
}
