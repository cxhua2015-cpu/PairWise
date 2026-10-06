package balanceledger262

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := validateOp(l.opts, op); err != nil {
			return err
		}
	}
	return nil
}
