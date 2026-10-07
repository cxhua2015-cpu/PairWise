package balanceledger437

type Stats struct {
	Generation, NextRevision uint64
	Accounts                 int
}

// Stats returns a linearizable summary of the current state.
func (l *Ledger) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return statsOf(&l.st)
}

func statsOf(st *state) Stats {
	return Stats{Generation: st.generation, NextRevision: st.nextRevision, Accounts: len(st.accounts)}
}
