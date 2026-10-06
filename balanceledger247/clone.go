package balanceledger247

// Clone returns a fully independent deep copy, including logical clocks.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	c := &Ledger{
		opts:       l.opts,
		accounts:   make(map[string]Account, len(l.accounts)),
		generation: l.generation,
		revision:   l.revision,
	}
	for name, acc := range l.accounts {
		c.accounts[name] = acc
	}
	return c, nil
}
