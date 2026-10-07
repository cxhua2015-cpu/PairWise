package balanceledger417

type Stats struct {
	Generation, NextRevision uint64
	Accounts                 int
}

// Stats returns a linearizable summary of the current state: it is taken
// under the read lock, so it always reflects a single point in the
// transaction history, consistent with concurrent Apply/Clone calls.
func (l *Ledger) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Stats{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     len(l.accounts),
	}
}
