package balanceledger432

type Stats struct {
	Generation, NextRevision uint64
	Accounts                 int
}

// Stats returns a linearizable summary of the current state.
func (l *Ledger) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return Stats{
		Generation:   l.state.generation,
		NextRevision: l.state.nextRevision,
		Accounts:     len(l.state.accounts),
	}
}
