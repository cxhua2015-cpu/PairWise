package balanceledger297

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the original: subsequent batches
// applied to either ledger never affect the other.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	c := &Ledger{
		opts:    l.opts,
		accts:   make(map[string]Account, len(l.accts)),
		gen:     l.gen,
		nextRev: l.nextRev,
	}
	for k, v := range l.accts {
		c.accts[k] = v
	}
	return c, nil
}
