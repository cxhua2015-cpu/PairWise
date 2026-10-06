package expirytable269

type Stats struct {
	Generation, NextRevision uint64
	Now                      int64
	Entries                  int
}

// Stats returns a linearizable summary of the current state: it observes
// the same mutex as Apply/Expire, so it never sees a half-committed
// candidate transaction.
func (t *Table) Stats() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Stats{
		Generation:   t.gen,
		NextRevision: t.nextRev,
		Now:          t.now,
		Entries:      len(t.entries),
	}
}
