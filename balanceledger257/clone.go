package balanceledger257

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the original.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accounts := make(map[string]Account, len(l.accounts))
	for name, a := range l.accounts {
		accounts[name] = a
	}
	return &Ledger{
		opts:         l.opts,
		accounts:     accounts,
		generation:   l.generation,
		nextRevision: l.nextRevision,
	}, nil
}
