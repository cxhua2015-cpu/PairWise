package balanceledger292

// Clone returns a fully independent deep copy, including logical clocks.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	c := &Ledger{
		opts:         l.opts,
		accounts:     make(map[string]Account, len(l.accounts)),
		generation:   l.generation,
		nextRevision: l.nextRevision,
	}
	for k, v := range l.accounts {
		c.accounts[k] = v
	}
	return c, nil
}
