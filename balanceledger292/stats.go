package balanceledger292

// Stats is a linearizable summary of the ledger state.
type Stats struct {
	Generation, NextRevision uint64
	Accounts                 int
}

// Stats returns a linearizable summary of the current state: it observes the
// logical clocks and the account count under the same read lock, so it never
// reflects a partially applied batch.
func (l *Ledger) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Stats{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     len(l.accounts),
	}
}
