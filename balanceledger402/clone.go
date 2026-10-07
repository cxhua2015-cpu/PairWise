package balanceledger402

// Clone returns a fully independent deep copy, including logical clocks
// (generation and next revision). The clone shares no memory with the
// original, so either ledger can be mutated without affecting the other.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accounts := make(map[string]account, len(l.accounts))
	for name, acc := range l.accounts {
		accounts[name] = acc
	}
	return &Ledger{
		opts:         l.opts,
		accounts:     accounts,
		generation:   l.generation,
		nextRevision: l.nextRevision,
	}, nil
}
