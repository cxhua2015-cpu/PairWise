package balanceledger412

// Clone returns a fully independent deep copy of the ledger, preserving the
// logical clocks (generation and next revision). No memory is shared with
// the original, so either ledger can be mutated without affecting the other.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accounts := make(map[string]Account, len(l.accounts))
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
