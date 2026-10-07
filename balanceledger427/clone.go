package balanceledger427

// Clone returns a fully independent deep copy, including logical clocks.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	m := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		m[k] = v
	}
	return &Ledger{
		opts:         l.opts,
		accounts:     m,
		generation:   l.generation,
		nextRevision: l.nextRevision,
	}, nil
}
