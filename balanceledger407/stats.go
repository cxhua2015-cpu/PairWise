package balanceledger407

type Stats struct {
	Generation, NextRevision uint64
	Accounts                 int
}

// Stats returns a linearizable summary of the current state: it is taken
// under the read lock, so it always reflects a single committed generation.
func (l *Ledger) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Stats{Generation: l.gen, NextRevision: l.nextRev, Accounts: len(l.accts)}
}
