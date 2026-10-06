package balanceledger232

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no memory with the original, so subsequent batches on
// either ledger never alias each other's state.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	c := &Ledger{
		opts:       l.opts,
		accounts:   make(map[string]accountState, len(l.accounts)),
		generation: l.generation,
		nextRev:    l.nextRev,
	}
	for k, v := range l.accounts {
		c.accounts[k] = v
	}
	return c, nil
}
