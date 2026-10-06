package balanceledger237

// Clone returns a fully independent deep copy, including the logical clocks
// (generation and nextRevision). The clone owns its own account index, so
// subsequent writes to either ledger never alias the other.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accounts := make(map[string]Account, len(l.accounts))
	for name, acc := range l.accounts {
		accounts[name] = acc
	}
	return &Ledger{
		opts:         l.opts,
		accounts:     accounts,
		generation:   l.generation,
		nextRevision: l.nextRevision,
	}, nil
}
