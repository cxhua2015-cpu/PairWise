package balanceledger437

// Clone returns a fully independent deep copy, including logical clocks.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make(map[string]Account, len(l.st.accounts))
	for k, v := range l.st.accounts {
		accs[k] = v
	}
	return &Ledger{opts: l.opts, st: state{accounts: accs, generation: l.st.generation, nextRevision: l.st.nextRevision}}, nil
}
