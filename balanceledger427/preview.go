package balanceledger427

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (l *Ledger) Preview(b Batch) (Result, Snapshot, Stats, error) {
	for _, op := range b.Ops {
		if err := l.validateOp(op); err != nil {
			return Result{}, Snapshot{}, Stats{}, err
		}
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	m := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		m[k] = v
	}
	rev, changed, last, err := l.apply(m, l.nextRevision, b.Ops)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	gen := l.generation
	if len(b.Ops) > 0 {
		gen++
	}
	cand := &Ledger{opts: l.opts, accounts: m, generation: gen, nextRevision: rev}
	res := Result{Generation: gen, Revision: last, Changed: changed}
	return res, cand.snapshotLocked(), cand.Stats(), nil
}
