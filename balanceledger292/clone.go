package balanceledger292

// Clone returns a fully independent deep copy of the ledger, including the
// logical clocks (generation and next revision). The clone shares no
// ownership with the original: subsequent mutations of either ledger never
// alias the other's state.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	c := &Ledger{
		opts:         l.opts,
		accounts:     make(map[string]Account, len(l.accounts)),
		generation:   l.generation,
		nextRevision: l.nextRevision,
	}
	for name, a := range l.accounts {
		c.accounts[name] = a
	}
	return c, nil
}
