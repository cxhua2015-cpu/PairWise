package balanceledger242

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if op.Kind != Add && op.Kind != Set && op.Kind != Delete {
			return ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Add:
			if op.Value != 0 || op.Delta == 0 {
				return ErrInvalidInput
			}
			if !absWithin(op.Delta, l.opts.MaxAbsValue) {
				return ErrValue
			}
		case Set:
			if op.Delta != 0 {
				return ErrInvalidInput
			}
			if !absWithin(op.Value, l.opts.MaxAbsValue) {
				return ErrValue
			}
		case Delete:
			if op.Delta != 0 || op.Value != 0 {
				return ErrInvalidInput
			}
		}
	}
	return nil
}
