package balanceledger432

// Clone returns a fully independent deep copy, including logical clocks
// (generation and nextRevision). The clone shares no memory with the
// receiver, so subsequent mutations of either ledger never alias.
func (l *Ledger) Clone() (*Ledger, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return &Ledger{opts: l.opts, state: l.state.copy()}, nil
}
