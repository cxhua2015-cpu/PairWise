package expirytable294

type Stats struct {
	Generation, NextRevision uint64
	Now                      int64
	Entries                  int
}

// Stats returns a linearizable summary of the current state.
func (t *Table) Stats() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Stats{
		Generation:   t.generation,
		NextRevision: t.nextRev,
		Now:          t.now,
		Entries:      len(t.entries),
	}
}
