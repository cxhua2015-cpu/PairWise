package expirytable439

type Stats struct {
	Generation, NextRevision uint64
	Now                      int64
	Entries                  int
}

// statsLocked summarizes the current state; caller must hold t.mu.
func (t *Table) statsLocked() Stats {
	return Stats{
		Generation:   t.generation,
		NextRevision: t.nextRevision,
		Now:          t.now,
		Entries:      len(t.entries),
	}
}

// Stats returns a linearizable summary of the current state.
func (t *Table) Stats() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.statsLocked()
}
