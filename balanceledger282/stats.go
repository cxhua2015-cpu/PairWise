package balanceledger282

type Stats struct {
	Generation, NextRevision uint64
	Accounts                 int
}

// Stats returns a linearizable summary of the current state.
func (l *Ledger) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Stats{Generation: l.generation, NextRevision: l.nextRevision, Accounts: len(l.accounts)}
}
