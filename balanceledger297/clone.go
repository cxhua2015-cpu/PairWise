package balanceledger297

// Clone returns a fully independent deep copy, including logical clocks.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	c := &Ledger{
		opts:    l.opts,
		accounts: make(map[string]*account, len(l.accounts)),
		gen:     l.gen,
		nextRev: l.nextRev,
	}
	for name, a := range l.accounts {
		cp := *a
		c.accounts[name] = &cp
	}
	return c, nil
}
