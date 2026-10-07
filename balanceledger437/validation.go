package balanceledger437

// ValidateBatch performs complete structural validation without reading or
// mutating ledger state. It shares the exact structural semantics used by
// Apply: known kinds, well-formed names within the configured byte limit,
// and non-zero deltas for Add.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := l.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}
