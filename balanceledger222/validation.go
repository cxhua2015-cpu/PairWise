package balanceledger222

// ValidateBatch performs complete structural validation without reading or
// mutating ledger state. It shares the exact structural semantics enforced
// by Apply: known kinds, no stray fields, and well-formed names.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if op.Delta == 0 || op.Value != 0 {
				return ErrInvalidInput
			}
		case Set:
			if op.Delta != 0 {
				return ErrInvalidInput
			}
		case Delete:
			if op.Delta != 0 || op.Value != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
		if !l.validName(op.Name) {
			return ErrInvalidInput
		}
	}
	return nil
}
