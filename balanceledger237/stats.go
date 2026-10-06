package balanceledger237

type Stats struct {
	Generation, NextRevision uint64
	Accounts                 int
}

// Stats returns a linearizable summary of the current state: the values are
// read under the same lock that serializes Apply, so they always reflect a
// single consistent point in the ledger's history.
func (l *Ledger) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Stats{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     len(l.accounts),
	}
}
