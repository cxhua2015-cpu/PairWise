package balanceledger437

// Stats is a linearizable summary of the ledger state.
type Stats struct {
	Generation, NextRevision uint64
	Accounts                 int
}

// Stats returns a consistent point-in-time summary of the current state.
func (l *Ledger) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Stats{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     len(l.accounts),
	}
}
