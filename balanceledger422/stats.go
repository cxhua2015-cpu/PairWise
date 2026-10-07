package balanceledger422

type Stats struct {
	Generation, NextRevision uint64
	Accounts                 int
}

// Stats returns a linearizable summary of the current state.
func (l *Ledger) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return statsOf(l.accounts, l.generation, l.nextRevision)
}

func statsOf(m map[string]Account, generation, nextRevision uint64) Stats {
	return Stats{Generation: generation, NextRevision: nextRevision, Accounts: len(m)}
}
