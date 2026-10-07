package balanceledger422

// Clone returns a fully independent deep copy, including logical clocks.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accts := make(map[string]Account, len(l.accts))
	for k, v := range l.accts {
		accts[k] = v
	}
	return &Ledger{
		opts:    l.opts,
		gen:     l.gen,
		nextRev: l.nextRev,
		accts:   accts,
	}, nil
}
