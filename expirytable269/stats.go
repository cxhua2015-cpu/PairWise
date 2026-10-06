package expirytable269

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
		Generation:   t.gen,
		NextRevision: t.nextRev,
		Now:          t.now,
		Entries:      len(t.entries),
	}
}
