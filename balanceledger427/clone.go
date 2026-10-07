package balanceledger427

// Clone returns a fully independent deep copy, including logical clocks.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cloneLocked(), nil
}

// cloneLocked copies state while the caller holds at least a read lock.
func (l *Ledger) cloneLocked() *Ledger {
	accounts := make(map[string]Account, len(l.accounts))
	for name, acc := range l.accounts {
		accounts[name] = acc
	}
	return &Ledger{
		opts:         l.opts,
		generation:   l.generation,
		nextRevision: l.nextRevision,
		accounts:     accounts,
	}
}
