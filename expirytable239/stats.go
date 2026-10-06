package expirytable239

type Stats struct {
	Generation, NextRevision uint64
	Now                      int64
	Entries                  int
}

// Stats returns a linearizable summary of the current state: it is
// computed under the same lock that serializes Apply and Expire, so it
// never observes a partially committed transaction.
func (t *Table) Stats() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Stats{
		Generation:   t.generation,
		NextRevision: t.nextRevision,
		Now:          t.now,
		Entries:      len(t.entries),
	}
}
