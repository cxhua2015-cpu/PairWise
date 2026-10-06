package balanceledger267

// Clone returns a fully independent deep copy, including logical clocks.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	accounts := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		accounts[k] = v
	}
	return &Ledger{
		opts:         l.opts,
		accounts:     accounts,
		generation:   l.generation,
		nextRevision: l.nextRevision,
	}, nil
}
