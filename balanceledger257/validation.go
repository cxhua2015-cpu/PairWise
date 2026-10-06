package balanceledger257

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Add:
			if op.Delta == 0 {
				return ErrInvalidInput
			}
			if !absOK(op.Delta, l.opts.MaxAbsValue) {
				return ErrValue
			}
		case Set:
			if !absOK(op.Value, l.opts.MaxAbsValue) {
				return ErrValue
			}
		}
	}
	return nil
}
