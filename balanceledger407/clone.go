package balanceledger407

// Clone returns a fully independent deep copy, including logical clocks.
// The clone owns its accounts map and mutex; later writes to either ledger
// never alias the other.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accts := make(map[string]Account, len(l.accts))
	for k, v := range l.accts {
		accts[k] = v
	}
	return &Ledger{opts: l.opts, accts: accts, gen: l.gen, nextRev: l.nextRev}, nil
}
