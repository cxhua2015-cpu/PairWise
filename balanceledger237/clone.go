package balanceledger237

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no ownership with the original: subsequent mutations
// of either ledger never alias the other's state.
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
