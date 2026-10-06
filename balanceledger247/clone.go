package balanceledger247

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no memory with the original; later mutations to either
// ledger never alias the other.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		accs[k] = v
	}
	return &Ledger{
		opts:         l.opts,
		accounts:     accs,
		generation:   l.generation,
		nextRevision: l.nextRevision,
	}, nil
}
